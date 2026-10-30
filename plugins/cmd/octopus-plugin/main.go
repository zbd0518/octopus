package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lingyuins/octopus/internal/plugin"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "pack":
		err = pack(os.Args[2:])
	case "validate":
		err = validate(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: octopus-plugin pack -dir ./plugin -out plugin.octopkg\n       octopus-plugin validate plugin.octopkg")
}

func pack(args []string) error {
	flags := flag.NewFlagSet("pack", flag.ContinueOnError)
	dir, out := flags.String("dir", "", "plugin source directory"), flags.String("out", "", "output .octopkg file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *out == "" {
		return fmt.Errorf("pack requires -dir and -out")
	}
	root, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	manifest, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return err
	}
	if _, err := plugin.ParseManifest(manifest); err != nil {
		return err
	}
	wasm, err := os.ReadFile(filepath.Join(root, "plugin.wasm"))
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range map[string][]byte{"manifest.json": manifest, "plugin.wasm": wasm} {
		if err := writeZIP(writer, name, content); err != nil {
			return err
		}
	}
	uiRoot := filepath.Join(root, "ui")
	if info, err := os.Stat(uiRoot); err == nil && info.IsDir() {
		err = filepath.Walk(uiRoot, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("UI assets must be regular files")
			}
			relative, err := filepath.Rel(root, filePath)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			return writeZIP(writer, filepath.ToSlash(relative), content)
		})
		if err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if _, err := plugin.ReadPackageBytes(buffer.Bytes()); err != nil {
		return err
	}
	if err := os.WriteFile(*out, buffer.Bytes(), 0600); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, buffer.Len())
	return nil
}

func writeZIP(writer *zip.Writer, name string, content []byte) error {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("unsafe plugin path")
	}
	entry, err := writer.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, bytes.NewReader(content))
	return err
}

func validate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("validate requires a package path")
	}
	data, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	pkg, err := plugin.ReadPackageBytes(data)
	if err != nil {
		return err
	}
	summary, _ := json.MarshalIndent(map[string]any{"id": pkg.Manifest.ID, "version": pkg.Manifest.Version, "permissions": pkg.Manifest.Permissions, "events": pkg.Manifest.Events, "wasm_sha256": plugin.WASMDigest(pkg.WASM)}, "", "  ")
	fmt.Println(string(summary))
	return nil
}
