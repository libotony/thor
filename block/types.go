package block

import "errors"

type Vote uint

const (
	WIT Vote = iota
	COM
)

func TestVote(v Vote) error {
	if v == COM || v == WIT {
		return nil
	}
	return errors.New("invalid BFT vote")
}
