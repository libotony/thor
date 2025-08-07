//SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

// Status is the status of a validation
enum Status {
    Unknown,
    Queued,
    Active,
    Exited
}
uint32 constant MAX_UINT32 = type(uint32).max;
uint256 constant STAKER_PAUSED_BIT = 1 << 0;
uint256 constant DELEGATOR_PAUSED_BIT = 1 << 1;

contract Staker {
    event ValidationQueued(address indexed validator, address indexed endorsor, uint32 period, uint256 stake);
    event ValidationWithdrawn(address indexed validator, uint256 stake);
    event ValidationSignaledExit(address indexed validator);
    event StakeIncreased(address indexed validator, uint256 added);
    event StakeDecreased(address indexed validator, uint256 removed);

    event DelegationAdded(address indexed validator, uint256 indexed delegationID, uint256 stake, uint8 multiplier);
    event DelegationWithdrawn(uint256 indexed delegationID, uint256 stake);
    event DelegationSignaledExit(uint256 indexed delegationID);

    /**
     * @dev totalStake returns all stakes and weight by active validators.
     */
    function totalStake() public view returns (uint256, uint256) {
        return StakerNative(address(this)).native_totalStake();
    }

    /**
     * @dev queuedStake returns all stakes and weight by queued validators.
     */
    function queuedStake() public view returns (uint256, uint256) {
        return StakerNative(address(this)).native_queuedStake();
    }

    /**
     * @dev addValidation creates a validation to the queue.
     */
    function addValidation(address validator, uint32 period) public payable stakerNotPaused checkStake(msg.value) {
        require(validator != address(0), "staker: invalid validator");
        (, , , Status status, , , , ) = StakerNative(address(this)).native_get(validator);
        require(status == Status.Unknown, "staker: validation exists");
        require(
            StakerNative(address(this)).native_validateStakeChange(validator, msg.value, 0),
            "staker: stake is out of range"
        );

        string memory error = StakerNative(address(this)).native_addValidation(
            validator,
            msg.sender,
            period,
            msg.value
        );
        require(bytes(error).length == 0, error);
        emit ValidationQueued(validator, msg.sender, period, msg.value);
    }

    /**
     * @dev increaseStake adds VET to the current stake of the queued/active validator.
     */
    function increaseStake(address validator) public payable stakerNotPaused checkStake(msg.value) {
        (address endorsor, , , Status status, , , , uint32 exitBlock) = StakerNative(address(this)).native_get(
            validator
        );
        require(status != Status.Unknown, "staker: validation not found");
        require(endorsor == msg.sender, "staker: endorsor required");
        require(status == Status.Active || status == Status.Queued, "staker: validation not active or queued");
        if (status == Status.Active) {
            require(exitBlock == MAX_UINT32, "staker: validation has signaled exit");
        }
        require(
            StakerNative(address(this)).native_validateStakeChange(validator, msg.value, 0),
            "staker: total stake reached max limit"
        );

        StakerNative(address(this)).native_increaseStake(validator, msg.value);
        emit StakeIncreased(validator, msg.value);
    }

    /**
     * @dev decreaseStake removes VET from the current stake of an active validator
     */
    function decreaseStake(address validator, uint256 amount) public stakerNotPaused checkStake(amount) {
        (address endorsor, , , Status status, , , , uint32 exitBlock) = StakerNative(address(this)).native_get(
            validator
        );
        require(status != Status.Unknown, "staker: validation not found");
        require(endorsor == msg.sender, "staker: endorsor required");
        require(status == Status.Active || status == Status.Queued, "staker: validation not active or queued");
        if (status == Status.Active) {
            require(exitBlock == MAX_UINT32, "staker: validation has signaled exit");
        }
        require(
            StakerNative(address(this)).native_validateStakeChange(validator, 0, amount),
            "staker: total stake is lower than min stake"
        );

        StakerNative(address(this)).native_decreaseStake(validator, amount);
        emit StakeDecreased(validator, amount);
    }

    /**
     * @dev allows the caller to withdraw a stake when their status is set to exited
     */
    function withdrawStake(address validator) public stakerNotPaused {
        (address endorsor, , , Status status, , , , ) = StakerNative(address(this)).native_get(validator);
        require(status != Status.Unknown, "staker: validation not found");
        require(status == Status.Active || status == Status.Queued, "staker: validation not active or queued");
        require(endorsor == msg.sender, "staker: endorsor required");

        uint256 withdrawable = StakerNative(address(this)).native_withdrawStake(validator);
        if (withdrawable > 0) {
            (bool success, ) = msg.sender.call{value: withdrawable}("");
            require(success, "staker: transfer failed");
            emit ValidationWithdrawn(validator, withdrawable);
        }
    }

    /**
     * @dev signalExit signals the intent to exit a validator position at the end of the staking period.
     */
    function signalExit(address validator) public stakerNotPaused {
        (address endorsor, , , Status status, , , , ) = StakerNative(address(this)).native_get(validator);
        require(status != Status.Unknown, "staker: validation not found");
        require(status == Status.Active, "staker: validation is not active");
        require(endorsor == msg.sender, "staker: endorsor required");

        StakerNative(address(this)).native_signalExit(validator);
        emit ValidationSignaledExit(validator);
    }

    /**
     * @dev addDelegation creates a delegation position on a validator.
     */
    function addDelegation(
        address validator,
        uint8 multiplier // (% of msg.value) 100 for x1, 200 for x2, etc. This enforces a maximum of 2.56x multiplier
    ) public payable onlyDelegatorContract delegatorNotPaused checkStake(msg.value) returns (uint256) {
        require(multiplier != 0 && multiplier < 200, "staker: invalid multiplier");
        (, , , Status status, , , , ) = StakerNative(address(this)).native_get(validator);
        require(status != Status.Unknown, "staker: validation not found");
        require(status == Status.Active || status == Status.Queued, "staker: validation not active or queued");
        require(
            StakerNative(address(this)).native_validateStakeChange(validator, msg.value, 0),
            "staker: total stake reached max limit"
        );

        uint256 delegationID = StakerNative(address(this)).native_addDelegation(validator, msg.value, multiplier);
        emit DelegationAdded(validator, delegationID, msg.value, multiplier);
        return delegationID;
    }

    /**
     * @dev exitDelegation signals the intent to exit a delegation position at the end of the staking period.
     * Funds are available once the current staking period ends.
     */
    // (validator, stake, startPeriod, endPeriod, multiplier, isLocked)
    function signalDelegationExit(uint256 delegationID) public onlyDelegatorContract delegatorNotPaused {
        (address validator, uint256 stake, , uint32 endPeriod, , bool locked) = StakerNative(address(this))
            .native_getDelegation(delegationID);
        require(stake > 0, "staker: delegation not found or withdrawn");
        require(endPeriod == MAX_UINT32, "staker: delegation already signaled exit");

        (, , , Status status, , , , ) = StakerNative(address(this)).native_get(validator);
        require(status != Status.Unknown, "staker: validation not found");

        // delegation and validation both valid, can only signal exit if locked
        require(locked, "staker: delegation is withdrawable");

        StakerNative(address(this)).native_signalDelegationExit(delegationID);
        emit DelegationSignaledExit(delegationID);
    }

    /**
     * @dev withdrawDelegation withdraws the delegation position funds.
     */
    function withdrawDelegation(uint256 delegationID) public onlyDelegatorContract delegatorNotPaused {
        (address validator, uint256 stake, , , , bool locked) = StakerNative(address(this)).native_getDelegation(
            delegationID
        );
        require(stake > 0, "staker: delegation not found or withdrawn");

        (, , , Status status, , , , ) = StakerNative(address(this)).native_get(validator);
        require(status != Status.Unknown, "staker: validation not found");

        require(!locked, "staker: delegation is not eligible for withdraw");

        uint256 withdrawable = StakerNative(address(this)).native_withdrawDelegation(delegationID);

        if (withdrawable > 0) {
            (bool success, ) = msg.sender.call{value: withdrawable}("");
            require(success, "staker: transfer failed"); // TODO: check if this is needed
            emit DelegationWithdrawn(delegationID, withdrawable);
        }
    }

    /**
     * @dev getDelegation returns the validator, stake, start and end period, multiplier and isLocked status of a delegation.
     * @return (validator, stake, startPeriod, endPeriod, multiplier, isLocked)
     */
    function getDelegation(uint256 delegationID) public view returns (address, uint256, uint32, uint32, uint8, bool) {
        (
            address validator,
            uint256 stake,
            uint32 startPeriod,
            uint32 endPeriod,
            uint8 multiplier,
            bool isLocked
        ) = StakerNative(address(this)).native_getDelegation(delegationID);

        return (validator, stake, startPeriod, endPeriod, multiplier, isLocked);
    }

    /**
     * @dev get returns the validator. endorsor, stake, weight, status, auto renew, online and staking period of a validator.
     * @return (validator, endorsor, stake, weight, status, online, stakingPeriod, startBlock, exitBlock)
     * - status (0: unknown, 1: queued, 2: active, 3: cooldown, 4: exited)
     */
    function get(
        address validator
    ) public view returns (address, uint256, uint256, Status, bool, uint32, uint32, uint32) {
        (
            address endorsor,
            uint256 stake,
            uint256 weight,
            Status status,
            bool online,
            uint32 period,
            uint32 startBlock,
            uint32 exitBlock
        ) = StakerNative(address(this)).native_get(validator);
        return (endorsor, stake, weight, status, online, period, startBlock, exitBlock);
    }

    /**
     * @dev getWithdrawable returns the amount of a validator's withdrawable VET.
     */
    function getWithdrawable(address validator) public view returns (uint256) {
        (, , , Status status, , , , ) = StakerNative(address(this)).native_get(validator);
        require(status != Status.Unknown, "staker: validation not found");

        return StakerNative(address(this)).native_getWithdrawable(validator);
    }

    /**
     * @dev firstActive returns the head validatorId of the active validators.
     */
    function firstActive() public view returns (address) {
        return StakerNative(address(this)).native_firstActive();
    }

    /**
     * @dev firstQueued returns the head validatorId of the queued validators.
     */
    function firstQueued() public view returns (address) {
        return StakerNative(address(this)).native_firstQueued();
    }

    /**
     * @dev next returns the validator in a linked list
     */
    function next(address prev) public view returns (address) {
        return StakerNative(address(this)).native_next(prev);
    }

    /**
     * @dev getDelegationRewards returns all delegator's rewards for a given validator address and staking period.
     */
    function getDelegationRewards(address validator, uint32 stakingPeriod) public view returns (uint256) {
        return StakerNative(address(this)).native_getDelegationRewards(validator, stakingPeriod);
    }

    /**
     * @dev getCompletedPeriods returns the number of completed periods for validation
     */
    function getCompletedPeriods(address validator) public view returns (uint32) {
        return StakerNative(address(this)).native_getCompletedPeriods(validator);
    }

    function getValidationTotals(address validator) public view returns (uint256, uint256, uint256, uint256) {
        (
            uint256 lockedStake,
            uint256 lockedWeight,
            uint256 delegatedStake,
            uint256 delegatedWeight
        ) = StakerNative(address(this)).native_getValidationTotals(validator);
        return (lockedStake, lockedWeight, delegatedStake, delegatedWeight);
    }

    modifier stakerNotPaused() {
        uint256 switches = StakerNative(address(this)).native_getControlSwitches();
        require((switches & STAKER_PAUSED_BIT) == 0, "staker: staker is paused");
        _;
    }

    modifier delegatorNotPaused() {
        uint256 switches = StakerNative(address(this)).native_getControlSwitches();
        require((switches & STAKER_PAUSED_BIT) == 0, "staker: staker is paused");
        require((switches & DELEGATOR_PAUSED_BIT) == 0, "staker: delegator is paused");
        _;
    }

    modifier onlyDelegatorContract() {
        require(msg.sender == StakerNative(address(this)).native_getDelegatorContract(), "staker: only delegator");
        _;
    }

    modifier checkStake(uint256 amount) {
        require(amount > 0, "staker: stake is empty");
        require(amount % 1e18 == 0, "staker: stake is not multiple of 1VET");
        _;
    }

    receive() external payable {
        revert("receive function not allowed");
    }

    fallback() external {
        revert("fallback function not allowed");
    }
}

interface StakerNative {
    // Write methods
    function native_addValidation(
        address validator,
        address endorsor,
        uint32 period,
        uint256 stake
    ) external returns (string calldata);

    function native_validateStakeChange(address validator, uint256 increase, uint256 decrease) external returns (bool);

    function native_increaseStake(address validator, uint256 amount) external;

    function native_decreaseStake(address validator, uint256 amount) external;

    function native_withdrawStake(address validator) external returns (uint256);

    function native_signalExit(address validator) external;

    function native_addDelegation(address validator, uint256 stake, uint8 multiplier) external returns (uint256);

    function native_withdrawDelegation(uint256 delegationID) external returns (uint256);

    function native_signalDelegationExit(uint256 delegationID) external;

    // Read methods
    function native_totalStake() external view returns (uint256, uint256);

    function native_queuedStake() external pure returns (uint256, uint256);

    function native_getDelegation(
        uint256 delegationID
    ) external view returns (address, uint256, uint32, uint32, uint8, bool);

    function native_get(
        address validator
    ) external view returns (address, uint256, uint256, Status, bool, uint32, uint32, uint32);

    function native_getWithdrawable(address validator) external view returns (uint256);

    function native_firstActive() external view returns (address);

    function native_firstQueued() external view returns (address);

    function native_next(address prev) external view returns (address);

    function native_getDelegationRewards(
        address validator,
        uint32 stakingPeriod
    ) external view returns (uint256);

    function native_getCompletedPeriods(address validator) external view returns (uint32);

    function native_getValidationTotals(
        address validator
    ) external view returns (uint256, uint256, uint256, uint256);

    function native_getDelegatorContract() external view returns (address);

    function native_getControlSwitches() external view returns (uint256);
}
