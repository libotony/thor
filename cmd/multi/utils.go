package main

import (
	"bufio"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"github.com/vechain/thor/thor"
	"gopkg.in/urfave/cli.v1"
)

var (
	configDirFlag = cli.StringFlag{
		Name:  "config-dir",
		Value: defaultConfigDir(),
		Usage: "directory for user global configurations",
	}
	numberFlag = cli.IntFlag{
		Name:  "number",
		Value: 5,
		Usage: "number of master keys",
	}
)

func defaultConfigDir() string {
	if home := homeDir(); home != "" {
		return filepath.Join(home, ".org.vechain.thor")
	}
	return ""
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if usr, err := user.Current(); err == nil {
		return usr.HomeDir
	}
	return ""
}

func makeConfigDir(ctx *cli.Context) (string, error) {
	dir := ctx.String(configDirFlag.Name)
	if dir == "" {
		return "", fmt.Errorf("unable to infer default config dir, use -%s to specify", configDirFlag.Name)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", errors.Wrapf(err, "create config dir [%v]", dir)
	}
	return dir, nil
}

func masterKeyPath(ctx *cli.Context) (string, error) {
	dir := ctx.String(configDirFlag.Name)
	if dir == "" {
		return "", fmt.Errorf("unable to infer default config dir, use -%s to specify", configDirFlag.Name)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", errors.Wrapf(err, "create config dir [%v]", dir)
	}

	return filepath.Join(dir, "multi-master.key"), nil
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func readMasters(path string) ([]*ecdsa.PrivateKey, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var keys []*ecdsa.PrivateKey
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		str := strings.TrimSpace(scanner.Text())
		if str == "" {
			continue
		}

		priv, err := crypto.HexToECDSA(str)
		if err != nil {
			return nil, err
		}

		keys = append(keys, priv)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

func loadMasters(ctx *cli.Context) error {
	path, err := masterKeyPath(ctx)
	if err != nil {
		return err
	}

	if exists, err := fileExists(path); err != nil {
		return err
	} else if !exists {
		return errors.New("key file does not exist")
	}

	keys, err := readMasters(path)
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		fmt.Println("loaded keys:")
		for _, priv := range keys {
			fmt.Println(thor.Address(crypto.PubkeyToAddress(priv.PublicKey)))
		}
	}

	return nil
}

func generateMasers(ctx *cli.Context) error {
	path, err := masterKeyPath(ctx)
	if err != nil {
		return err
	}

	if exists, err := fileExists(path); err != nil {
		return err
	} else if exists {
		return errors.New("key file already exist")
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	num := ctx.Int(numberFlag.Name)
	if num < 1 {
		return errors.New("invalid number")
	}

	for i := 0; i < num; i++ {
		priv, err := crypto.GenerateKey()
		if err != nil {
			return err
		}

		if _, err := file.WriteString(hex.EncodeToString(crypto.FromECDSA(priv)) + "\n"); err != nil {
			return err
		}
	}

	fmt.Println("successfully gerated keys:")
	keys, err := readMasters(path)
	if err != nil {
		return err
	}

	for _, priv := range keys {
		fmt.Println(thor.Address(crypto.PubkeyToAddress(priv.PublicKey)))
	}

	return nil
}
