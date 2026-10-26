// dwb is the DesktopWidgets build tool. It discovers widgets, generates their
// entry points, compiles and packages them per target, and maintains the
// release catalog. Run it from anywhere inside the repository:
//
//	go run ./tools/dwb <command> [flags]
package main

import (
	"fmt"
	"os"
)

type command struct {
	name, args, help string
	run              func(args []string) error
}

var commands = []command{
	{"list", "[--json]", "List widgets and their versions", cmdList},
	{"validate", "", "Validate all manifests, project.yml and platforms.yml", cmdValidate},
	{"new", "<id> [--name Name]", "Create a widget from widgets/_template", cmdNew},
	{"run", "<id>", "Run a widget locally (dev build, no update checks)", cmdRun},
	{"gen", "<id>", "Generate gen/<id> (entry point and embedded files)", cmdGen},
	{"targets", "[--json]", "Show the expanded build matrix", cmdTargets},
	{"plan", "--previous <file|url> [--all] [--github-output]", "Decide which widgets to build for a release", cmdPlan},
	{"build", "--target <id> [--widgets a,b] [--out dist]", "Compile and package widgets for one target", cmdBuild},
	{"catalog", "--tag <tag> --previous <file|url> [--dist dist]", "Merge built assets into catalog.json", cmdCatalog},
	{"readme", "--catalog <file> --out <file>", "Render the release repository README", cmdReadme},
	{"check-versions", "--base <git-ref>", "Fail if a widget changed without a version bump", cmdCheckVersions},
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage()
		return
	}
	for _, c := range commands {
		if c.name == os.Args[1] {
			if err := c.run(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "dwb %s: %v\n", c.name, err)
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "dwb: unknown command %q\n\n", os.Args[1])
	usage()
	os.Exit(2)
}

func usage() {
	fmt.Println("usage: dwb <command> [flags]\n\ncommands:")
	for _, c := range commands {
		fmt.Printf("  %-15s %s\n  %-15s   %s\n", c.name, c.help, "", c.args)
	}
}
