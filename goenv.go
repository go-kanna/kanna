package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// goEnv reads the toolchain and flags of the go command in dir, including
// settings saved with go env -w.
func goEnv(ctx context.Context, dir string) (string, []string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "-json", "GOROOT", "GOFLAGS")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("run go env: %w", err)
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return "", nil, fmt.Errorf("read go env: %w", err)
	}
	tags, err := buildTags(env["GOFLAGS"])
	if err != nil {
		return "", nil, fmt.Errorf("read GOFLAGS: %w", err)
	}
	return env["GOROOT"], tags, nil
}

func buildTags(flags string) ([]string, error) {
	words, err := splitFlags(flags)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, word := range words {
		name, value, hasValue := strings.Cut(word, "=")
		if name != "-tags" && name != "--tags" {
			continue
		}
		if !hasValue {
			return nil, fmt.Errorf("%s needs a value, as -tags=tag1,tag2", name)
		}
		tags = nil
		if strings.ContainsAny(value, " '") {
			tags, err = splitFlags(value)
			if err != nil {
				return nil, err
			}
			continue
		}
		for tag := range strings.SplitSeq(value, ",") {
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return tags, nil
}

// splitFlags follows the go command's GOFLAGS quoting: a whole word may be
// single- or double-quoted, with no expansion or unescaping inside it.
func splitFlags(text string) ([]string, error) {
	var words []string
	for text = strings.TrimLeft(text, " \t\r\n"); text != ""; text = strings.TrimLeft(text, " \t\r\n") {
		if text[0] == '\'' || text[0] == '"' {
			quote := text[0]
			text = text[1:]
			end := strings.IndexByte(text, quote)
			if end < 0 {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			words = append(words, text[:end])
			text = text[end+1:]
			continue
		}
		end := strings.IndexAny(text, " \t\r\n")
		if end < 0 {
			end = len(text)
		}
		words = append(words, text[:end])
		text = text[end:]
	}
	return words, nil
}
