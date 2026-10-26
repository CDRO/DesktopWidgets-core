package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
)

func cmdList(args []string) error {
	fl := flag.NewFlagSet("list", flag.ExitOnError)
	asJSON := fl.Bool("json", false, "print JSON")
	fl.Parse(args)
	r, err := loadRepo()
	if err != nil {
		return err
	}
	if *asJSON {
		var out []any
		for _, u := range r.Units {
			out = append(out, map[string]any{"id": u.M.ID, "kind": u.M.Kind, "name": u.M.Name, "version": u.M.ParsedVersion().String(), "dir": u.Dir})
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tVERSION\tNAME")
	for _, u := range r.Units {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.M.ID, u.M.Kind, u.M.ParsedVersion(), u.M.Name)
	}
	return tw.Flush()
}

func cmdValidate(args []string) error {
	r, err := loadRepo()
	if err != nil {
		return err
	}
	fmt.Printf("ok: %d units, %d targets\n", len(r.Units), len(r.Targets))
	return nil
}

func cmdTargets(args []string) error {
	fl := flag.NewFlagSet("targets", flag.ExitOnError)
	asJSON := fl.Bool("json", false, "print JSON")
	fl.Parse(args)
	r, err := loadRepo()
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r.Targets)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tOS/ARCH\tRUNNER\tCONTAINER\tFORMATS")
	for _, t := range r.Targets {
		fmt.Fprintf(tw, "%s\t%s/%s\t%s\t%s\t%s\n", t.ID, t.OS, t.Arch, t.Runner, t.Container, strings.Join(t.Formats, ","))
	}
	return tw.Flush()
}

var newID = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

func cmdNew(args []string) error {
	fl := flag.NewFlagSet("new", flag.ExitOnError)
	name := fl.String("name", "", "display name")
	if len(args) == 0 {
		return fmt.Errorf("usage: dwb new <id> [--name Name]")
	}
	id := args[0]
	fl.Parse(args[1:])
	if !newID.MatchString(id) {
		return fmt.Errorf("id %q must be kebab-case", id)
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	dst := filepath.Join(root, "widgets", id)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if *name == "" {
		words := strings.Split(id, "-")
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		*name = strings.Join(words, " ")
	}
	pkg := strings.ReplaceAll(id, "-", "")
	src := filepath.Join(root, "widgets", "_template")
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(p, ".png") {
			s := strings.NewReplacer("__ID__", id, "__NAME__", *name, "__PKG__", pkg).Replace(string(data))
			data = []byte(s)
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		return err
	}
	fmt.Printf("created widgets/%s — run it with: go run ./tools/dwb run %s\n", id, id)
	return nil
}

func cmdGen(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dwb gen <id>")
	}
	r, err := loadRepo()
	if err != nil {
		return err
	}
	u, err := r.unit(args[0])
	if err != nil {
		return err
	}
	dir, err := generate(r, u, genOptions{Release: true})
	if err == nil {
		fmt.Println(dir)
	}
	return err
}

func cmdRun(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dwb run <id>")
	}
	r, err := loadRepo()
	if err != nil {
		return err
	}
	u, err := r.unit(args[0])
	if err != nil {
		return err
	}
	if _, err := generate(r, u, genOptions{}); err != nil {
		return err
	}
	cmd := exec.Command("go", "run", "-tags", "gtk3", "./gen/"+u.M.ID)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = r.Root, os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
