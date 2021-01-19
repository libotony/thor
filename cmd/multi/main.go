package main

import (
	"fmt"
	"os"

	"gopkg.in/urfave/cli.v1"
)

var (
	version   string
	gitCommit string
	gitTag    string
)

func fullVersion() string {
	versionMeta := "release"
	if gitTag == "" {
		versionMeta = "dev"
	}
	return fmt.Sprintf("%s-%s-%s", version, gitCommit, versionMeta)
}

func main() {
	app := cli.App{
		Version:   fullVersion(),
		Name:      "Thor",
		Usage:     "Multiple master key management",
		Copyright: "2018 VeChain Foundation <https://vechain.org/>",
		Flags: []cli.Flag{
			configDirFlag,
		},
		Action: loadMasters,
		Commands: []cli.Command{
			{
				Name:  "generate",
				Usage: "Generate master keys",
				Flags: []cli.Flag{
					configDirFlag,
					numberFlag,
				},
				Action: generateMasers,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
