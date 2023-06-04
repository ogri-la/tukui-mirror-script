package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"testing"

	"github.com/stretchr/testify/assert"
)

type DummyMirror struct {
	IMirror
}

func (i DummyMirror) fetch_addon_list() []Addon {
	path := "testdata/api-resp.json"
	fixture, err := ioutil.ReadFile(path)
	panicOnErr(err, "loading test fixture")
	var addon_list []Addon
	err = json.Unmarshal(fixture, &addon_list)
	panicOnErr(err, "unmarshalling test fixture into a list of Addon structs")
	return addon_list
}

// downloads zip file at `url` to file, returning the output path and panicking on any errors.
func (app DummyMirror) download_addon(addon Addon, output_path string) string {
	if addon.Slug == "tukui-dummy" {
		return "testdata/tukui-dummy.zip"
	}
	if addon.Slug == "elvui-dummy" {
		return "testdata/elvui-dummy.zip"
	}
	panic("failed to 'download' addon using dummy interface: " + addon.Slug)
}

func (app DummyMirror) fetch_repo(addon Addon, script_path string) {
	revision, _ := map[string]string{
		"tukui-dummy": "b0492cc",
		"elvui-dummy": "dc06d5f",
	}[addon.Slug]

	rc, so, se := run_cmd(fmt.Sprintf("reset-dummy-repo.sh %s %s", addon.Slug, revision), script_path)
	ensure(rc == 0, "failed to reset dummy repo: "+so+se)
}

func reset_dummy_repositories(script_path string) {
	run_all_cmd([]string{}, script_path)
}

func TestMirror(t *testing.T) {
	script_path, err := os.Getwd()

	panicOnErr(err, "fetching the current working directory")
	// token := github_token()
	token := "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"

	app := DummyMirror{}
	mirror(app, script_path, token)
	assert.True(t, true)
}
