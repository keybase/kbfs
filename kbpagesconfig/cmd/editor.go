// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/keybase/client/go/minterm"
	"github.com/keybase/kbfs/libpages/config"
	"github.com/sergi/go-diff/diffmatchpatch"
	"golang.org/x/crypto/bcrypt"
)

var term *minterm.MinTerm

func init() {
	var err error
	if term, err = minterm.New(); err != nil {
		fmt.Fprintf(os.Stderr, "opening terminal error: %s\n", err)
		os.Exit(1)
	}
}

// not go-routine safe!
type kbpConfigEditor struct {
	kbpConfig         *config.V1
	originalConfigStr string
}

func readConfig(from io.ReadCloser) (cfg config.Config, str string, err error) {
	buf := &bytes.Buffer{}
	if cfg, err = config.ParseConfig(io.TeeReader(from, buf)); err != nil {
		return nil, "", err
	}
	if err = from.Close(); err != nil {
		return nil, "", err
	}
	return cfg, buf.String(), nil
}

// initConfig reads in config file and ENV variables if set.
func initKBPConfigEditorOrBust() {
	editor = &kbpConfigEditor{}
	f, err := os.Open(kbpConfigPath)
	switch {
	case err == nil:
		var cfg config.Config
		cfg, editor.originalConfigStr, err = readConfig(f)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"reading config file %s error: %v", kbpConfigPath, err)
			os.Exit(1)
		}
		if cfg.Version() != config.Version1 {
			fmt.Fprintf(os.Stderr,
				"unsupported config version %s", cfg.Version())
			os.Exit(1)
		}
		editor.kbpConfig = cfg.(*config.V1)
	case os.IsNotExist(err):
		editor.kbpConfig = config.DefaultV1()
	default:
		fmt.Fprintf(os.Stderr,
			"open file %s error: %v", kbpConfigPath, err)
		os.Exit(1)
	}
}

func (e *kbpConfigEditor) confirmAndWrite() error {
	buf := &bytes.Buffer{}
	if err := e.kbpConfig.Encode(buf, true); err != nil {
		return fmt.Errorf("encoding config error: %v", err)
	}
	newConfigStr := buf.String()
	if newConfigStr == e.originalConfigStr {
		fmt.Println("no change is made to the config")
		return nil
	}

	// print the diff
	d := diffmatchpatch.New()
	fmt.Println(
		d.DiffPrettyText(
			d.DiffMain(e.originalConfigStr, newConfigStr, true)))

	// ask user to confirm
	input, err := term.Prompt(fmt.Sprintf(
		"confirm writing above changes to %s? (y/N): ", kbpConfigPath))
	if err != nil {
		return fmt.Errorf("getting confirmation error: %v", err)
	}
	if strings.ToLower(input) != "y" {
		return fmt.Errorf("write not confirmed")
	}

	// write the new config to kbpConfigPath
	f, err := os.Create(kbpConfigPath)
	if err != nil {
		return fmt.Errorf("opening file [%s] error: %v", kbpConfigPath, err)
	}
	if _, err = f.WriteString(newConfigStr); err != nil {
		return fmt.Errorf(
			"writing config to file [%s] error: %v", kbpConfigPath, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("closing file [%s] error: %v", kbpConfigPath, err)
	}

	return nil
}

func (e *kbpConfigEditor) addUser(username string) error {
	if _, ok := e.kbpConfig.Users[username]; ok {
		return fmt.Errorf("user %s already exists", username)
	}
	input, err := term.PromptPassword(fmt.Sprintf(
		"enter a password for %s: ", username))
	if err != nil {
		return fmt.Errorf("getting password error: %v", err)
	}
	password := strings.TrimSpace(input)
	if len(password) == 0 {
		return fmt.Errorf("empty password")
	}
	hashed, err := bcrypt.GenerateFromPassword(
		[]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password error: %v", err)
	}
	if e.kbpConfig.Users == nil {
		e.kbpConfig.Users = make(map[string]string)
	}
	e.kbpConfig.Users[username] = string(hashed)
	return nil
}

func (e *kbpConfigEditor) removeUser(username string) {
	delete(e.kbpConfig.Users, username)
}

func (e *kbpConfigEditor) setAnonymousPermission(
	permsStr string, pathStr string) error {
	if e.kbpConfig.ACLs == nil {
		e.kbpConfig.ACLs = make(map[string]config.AccessControlV1)
	}
	pathACL := e.kbpConfig.ACLs[pathStr] // struct
	pathACL.AnonymousPermissions = permsStr
	e.kbpConfig.ACLs[pathStr] = pathACL
	return e.kbpConfig.Validate()
}

func (e *kbpConfigEditor) clearACL(pathStr string) {
	delete(e.kbpConfig.ACLs, pathStr)
}

func (e *kbpConfigEditor) setAdditionalPermission(
	username string, permsStr string, pathStr string) error {
	if e.kbpConfig.ACLs == nil {
		e.kbpConfig.ACLs = make(map[string]config.AccessControlV1)
	}
	pathACL := e.kbpConfig.ACLs[pathStr] // struct
	if pathACL.WhitelistAdditionalPermissions == nil {
		// If permsStr is empty, we'd leave an empty permission entry behind.
		// But that's OK. If user really wants it gone, they can use the
		// "remove" command.
		pathACL.WhitelistAdditionalPermissions = make(map[string]string)
	}
	pathACL.WhitelistAdditionalPermissions[username] = permsStr
	e.kbpConfig.ACLs[pathStr] = pathACL
	return e.kbpConfig.Validate()
}

func (e *kbpConfigEditor) removeUserFromACL(username string, pathStr string) {
	if e.kbpConfig.ACLs == nil {
		return
	}
	if e.kbpConfig.ACLs[pathStr].WhitelistAdditionalPermissions == nil {
		return
	}
	delete(e.kbpConfig.ACLs[pathStr].WhitelistAdditionalPermissions, username)
}

func (e *kbpConfigEditor) checkUserOnPath(
	username string, pathStr string) (read, list bool, err error) {
	read, list, _, err = e.kbpConfig.GetPermissionsForUsername(pathStr, username)
	return read, list, err
}
