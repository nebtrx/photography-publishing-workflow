package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type Output struct {
	OK   bool     `json:"ok"`
	Info []string `json:"info,omitempty"`
}

func main() {
	var (
		inPath  = flag.String("input", "", "input path")
		outPath = flag.String("out", "", "output path")
		dryRun  = flag.Bool("dry-run", false, "do not write output; print intent only")
	)
	flag.Parse()

	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --input and --out are required")
		os.Exit(2)
	}

	// TODO: implement deterministic logic. Keep tools small and explicit.
	out := Output{OK: true, Info: []string{"template tool ran"}}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: marshal:", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Printf("DRY RUN: would write %s (%d bytes)\n", *outPath, len(b))
		os.Exit(0)
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: mkdir:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: write:", err)
		os.Exit(1)
	}
	fmt.Println("Wrote:", *outPath)
}
