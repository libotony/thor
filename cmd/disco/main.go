// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// disco runs a bootstrap node for the Ethereum Discovery Protocol.
package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"net"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/p2p/netutil"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v3"

	"github.com/vechain/thor/v2/cmd/thor/httpserver"
	"github.com/vechain/thor/v2/metrics"
	"github.com/vechain/thor/v2/p2p/discv5"
	"github.com/vechain/thor/v2/p2psrv"
)

var (
	version       string
	gitCommit     string
	gitTag        string
	copyrightYear string

	flags = []cli.Flag{
		addrFlag,
		keyFileFlag,
		keyHexFlag,
		natFlag,
		netRestrictFlag,
		nodedbFlag,
		verbosityFlag,
		enableMetricsFlag,
		metricsAddrFlag,
	}
)

func run(_ context.Context, ctx *cli.Command) error {
	lvl, err := readIntFromUInt64Flag(ctx.Uint64(verbosityFlag.Name))
	if err != nil {
		return errors.Wrap(err, "parse verbosity flag")
	}
	initLogger(lvl)

	natm, err := nat.Parse(ctx.String("nat"))
	if err != nil {
		return errors.Wrap(err, "-nat")
	}

	var key *ecdsa.PrivateKey

	if keyHex := ctx.String("keyhex"); keyHex != "" {
		if key, err = crypto.HexToECDSA(keyHex); err != nil {
			return errors.Wrap(err, "-keyhex")
		}
	} else {
		if key, err = loadOrGenerateKeyFile(ctx.String("keyfile")); err != nil {
			return errors.Wrap(err, "-keyfile")
		}
	}

	netrestrict := ctx.String("netrestrict")
	var restrictList *netutil.Netlist
	if netrestrict != "" {
		restrictList, err = netutil.ParseNetlist(netrestrict)
		if err != nil {
			return errors.Wrap(err, "-netrestrict")
		}
	}

	addr, err := net.ResolveUDPAddr("udp", ctx.String("addr"))
	if err != nil {
		return errors.Wrap(err, "-addr")
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}

	realAddr := conn.LocalAddr().(*net.UDPAddr)
	if natm != nil {
		if !realAddr.IP.IsLoopback() {
			go nat.Map(natm, nil, "udp", realAddr.Port, realAddr.Port, "ethereum discovery")
		}
		// TODO: react to external IP changes over time.
		if ext, err := natm.ExternalIP(); err == nil {
			realAddr = &net.UDPAddr{IP: ext, Port: realAddr.Port}
		}
	}
	unhandled := make(chan discover.ReadPacket, 100)
	network, err := discv5.ListenUDP(key, conn, realAddr, "", restrictList, unhandled)
	if err != nil {
		return err
	}
	ln, err := newLocalNode(key, realAddr, ctx.String(nodedbFlag.Name))
	if err != nil {
		network.Close()
		return err
	}
	v5, err := discover.ListenV5(&discover.SharedUDPConn{UDPConn: conn, Unhandled: unhandled}, ln, discover.Config{
		PrivateKey:   key,
		NetRestrict:  restrictList,
		V5ProtocolID: &p2psrv.V5ProtocolID,
	})
	if err != nil {
		network.Close()
		ln.Database().Close()
		return err
	}
	// legacy first: v5.Close waits for its read loop, which ends only once legacy closes unhandled
	defer func() {
		network.Close()
		v5.Close()
		ln.Database().Close()
	}()
	fmt.Println("Running", network.Self().String())
	fmt.Println("ENR", ln.Node().String())
	if !ln.Node().IPAddr().IsValid() {
		fmt.Println("warning: node record has no IP; pass --nat extip:<IP> to publish a usable ENR")
	}

	exitSignal := handleExitSignal()

	if ctx.Bool(enableMetricsFlag.Name) {
		metrics.InitializePrometheusMetrics()
		url, closeFunc, err := httpserver.StartMetricsServer(ctx.String(metricsAddrFlag.Name))
		if err != nil {
			return fmt.Errorf("unable to start metrics server - %w", err)
		}
		fmt.Println("metrics server listening", url)
		defer closeFunc()
		go pollMetrics(exitSignal, network, v5)
	}

	<-exitSignal.Done()

	return nil
}

func main() {
	versionMeta := "release"
	if gitTag == "" {
		versionMeta = "dev"
	}
	app := cli.Command{
		Version:   fmt.Sprintf("%s-%s-%s", version, gitCommit, versionMeta),
		Name:      "Disco",
		Usage:     "VeChain Thor bootstrap node",
		Copyright: fmt.Sprintf("2018-%s VeChain Foundation <https://vechain.org/>", copyrightYear),
		Flags:     flags,
		Action:    run,
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
