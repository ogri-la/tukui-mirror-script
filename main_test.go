/*
   Copyright (C) 2023 Torkus

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as
   published by the Free Software Foundation, either version 3 of the
   License, or (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"golang.org/x/oauth2"

	"github.com/google/go-github/v52/github"

	"testing"
)

type DummyMirror struct {
	IMirror
}

type DummyMirror2 struct {
	IMirror
}

func _fetch_addon_list(fixture_path string) []Addon {
	fixture, err := ioutil.ReadFile(fixture_path)
	panicOnErr(err, "loading test fixture")
	var addon_list []Addon
	err = json.Unmarshal(fixture, &addon_list)
	panicOnErr(err, "unmarshalling test fixture into a list of Addon structs")
	return addon_list
}

func (i DummyMirror) fetch_addon_list() []Addon {
	return _fetch_addon_list("testdata/api-resp.json")
}

func (i DummyMirror2) fetch_addon_list() []Addon {
	return _fetch_addon_list("testdata/api-resp2.json")
}

func _download_addon(addon Addon, output_path string) string {
	if addon.Slug == "tukui-dummy" {
		return "testdata/tukui-dummy.zip"
	}
	if addon.Slug == "elvui-dummy" {
		return "testdata/elvui-dummy.zip"
	}
	panic("failed to 'download' addon using dummy interface: " + addon.Slug)
}

// downloads zip file at `url` to file, returning the output path and panicking on any errors.
func (app DummyMirror) download_addon(addon Addon, output_path string) string {
	return _download_addon(addon, output_path)
}

// downloads zip file at `url` to file, returning the output path and panicking on any errors.
func (app DummyMirror2) download_addon(addon Addon, output_path string) string {
	return _download_addon(addon, output_path)
}

func reset(token string) {
	script_path, err := os.Getwd()
	panicOnErr(err, "fetching the current working directory")
	addon_list := []Addon{
		Addon{Slug: "tukui-dummy"},
		Addon{Slug: "elvui-dummy"},
	}
	for _, addon := range addon_list {
		// delete any Github releases.
		// if you reset the repos first, it leaves draft releases behind(?)
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		tc := oauth2.NewClient(ctx, ts)
		client := github.NewClient(tc)
		// https://docs.github.com/en/rest/releases/releases?apiVersion=2022-11-28#delete-a-release
		opts := github.ListOptions{}
		release_list, _, _ := client.Repositories.ListReleases(ctx, "ogri-la", addon.Slug, &opts)
		for _, release := range release_list {
			_, err := client.Repositories.DeleteRelease(ctx, "ogri-la", addon.Slug, release.GetID())
			panicOnErr(err, "deleting release")
		}

		// reset remote repository to initial commit, delete any local and remote tags
		revision, _ := map[string]string{
			"tukui-dummy": "b0492cc",
			"elvui-dummy": "dc06d5f",
		}[addon.Slug]
		rc, so, se := run_cmd(fmt.Sprintf("./reset-dummy-repo.sh %s %s", addon.Slug, revision), script_path)
		ensure(rc == 0, "failed to reset dummy repo: "+so+se)
	}
}

func TestMirror(t *testing.T) {
	token := github_token()

	reset(token)

	release_one := DummyMirror{}
	mirror(release_one, token)

	release_two := DummyMirror2{}
	mirror(release_two, token)
}
