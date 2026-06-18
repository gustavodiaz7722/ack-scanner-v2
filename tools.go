//go:build tools

package main

// This file ensures tool/test dependencies are tracked in go.mod.
// These packages are used by other packages in this module but may not
// be imported by code that is built by default.
import (
	_ "github.com/google/go-github/v60/github"
	_ "golang.org/x/oauth2"
	_ "gopkg.in/yaml.v3"
	_ "pgregory.net/rapid"
)
