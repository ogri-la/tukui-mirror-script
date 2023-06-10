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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/go-github/v52/github"
	"golang.org/x/oauth2"
)

type Addon struct {
	Slug      string   `json:"slug"` // "elvui"
	Name      string   `json:"name"` // "ElvUI"
	Url       string   `json:"url"`
	Version   string   `json:"version"`
	PatchList []string `json:"patch"`
}

// ---

type IMirror interface {
	fetch_addon_list() []Addon
	download_addon(addon Addon, output_path string) string
}

type TukuiMirror struct {
	IMirror
}

// ---

func panicOnErr(err error, doing string) {
	if err != nil {
		panic(fmt.Sprintf("failed while %s: %s", doing, err.Error()))
	}
}

func ensure(b bool, msg string) {
	if !b {
		panic(msg)
	}
}

func stdout(msg string) {
	fmt.Println(msg)
}

func stderr(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}

// returns `true` if file at `path` exists.
func file_exists(path string) bool {
	_, err := os.Stat(path)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

// 10.1.0 => mainline, 1.14.3 => classic, etc
func patch_to_flavour(patch string) string {
	entry, present := map[string]string{
		"1.": "classic",
		"2.": "bcc",
		"3.": "wrath",
	}[patch[:2]] // "1.14.3" => "1."
	if !present {
		return "mainline"
	}
	return entry
}

// 10.1.0 => 100100, 1.14.3 => 11400, etc
func patch_to_interface(patch string) int {
	bits := strings.SplitN(patch, ".", 3)
	ensure(len(bits) > 1, "failed to parse game version: "+patch)
	major, err := strconv.Atoi(bits[0])
	panicOnErr(err, "parsing major portion of game version")
	minor, err := strconv.Atoi(bits[1])
	panicOnErr(err, "parsing minor portion of game version")
	return (10000 * major) + (100 * minor)
}

// run a shell command, returning the return code, stdout and stderr.
func run_cmd(command string, path string) (int, string, string) {
	// cmd_bits := strings.Split(command, " ")
	// cmd := exec.Command(cmd_bits[0], cmd_bits[1:]...)
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = path
	var _stdout, _stderr strings.Builder
	cmd.Stdout = &_stdout
	cmd.Stderr = &_stderr
	rc := 0
	stderr(fmt.Sprintf("executing %s in %s", cmd.String(), path))
	err := cmd.Run()

	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if ok {
			rc = exitError.ExitCode()
		}
	}
	return rc, strings.TrimSpace(_stdout.String()), strings.TrimSpace(_stderr.String())
}

func run_all_cmd(command_list []string, cwd string) {
	for _, cmd := range command_list {
		rc, _, _stderr := run_cmd(cmd, cwd)
		ensure(rc == 0, fmt.Sprintf("command '%s' failed: %s", cmd, _stderr))
	}
}

// ---

func (i TukuiMirror) fetch_addon_list() []Addon {
	url := "https://api.tukui.org/v1/addons"
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	panicOnErr(err, "composing request to fetch addon list")
	req.Header.Set("User-Agent", "tukui-mirror-script/1.x (https://github.com/ogri-la/tukui-mirror-script)")
	resp, err := client.Do(req)
	panicOnErr(err, "fetching addon list")
	defer resp.Body.Close()

	body_bytes, err := ioutil.ReadAll(resp.Body)
	panicOnErr(err, "reading response body into a byte array")
	ensure(resp.StatusCode == 200, fmt.Sprintf("non-200 response fetching addon list (%d): %s", resp.StatusCode, string(body_bytes)))
	addon_list := []Addon{}
	err = json.Unmarshal(body_bytes, &addon_list)
	panicOnErr(err, "deserialising response bytes into a list of addon structs")

	return addon_list
}

// downloads zip file at `url` to a destination within `output_path`,
// returning the final output path.
func (app TukuiMirror) download_addon(addon Addon, output_path string) string {
	// "elvui--13.33.zip", "tukui--20.37.zip"
	addon_filename := fmt.Sprintf("%s--%s.zip", addon.Slug, addon.Version)
	// "/path/to/output/dir/elvui--13.33.zip"
	zip_output_path := filepath.Join(output_path, addon_filename)
	if file_exists(zip_output_path) {
		// stderr("addon zip file exists, not downloading: " + zip_output_path)
		return zip_output_path
	}
	stderr("downloading: " + addon.Url)
	resp, err := http.Get(addon.Url)
	panicOnErr(err, "downloading addon to file")
	defer resp.Body.Close()
	ensure(resp.StatusCode == 200, "non-200 response downloading zip file")

	zip_bytes, err := ioutil.ReadAll(resp.Body)
	panicOnErr(err, "reading bytes from response body")
	err = ioutil.WriteFile(zip_output_path, zip_bytes, os.FileMode(int(0644)))
	panicOnErr(err, "writing bytes to zip file")
	stderr("wrote: " + zip_output_path)
	return zip_output_path
}

// creates a 'release.json' JSON string for the given `addon`.
func gen_release_json(addon Addon, zip_output_filename string) string {
	release_json := `{
    "releases": [
        {
            "name": "%s",
            "version": "%s",
            "filename": "%s",
            "nolib": false,
            "metadata": [%s
            ]
        }
    ]
}
`
	release_flavour_json := `
                {
                    "flavor": "%s",
                    "interface": %d
                }`
	metadata := []string{}
	for _, patch := range addon.PatchList {
		flavour := patch_to_flavour(patch) // 10.1.0 => mainline, 1.13.3 => classic
		iface := patch_to_interface(patch) // 10.1.0 => 100100, 1.13.3 => 11300
		metadata = append(metadata, fmt.Sprintf(release_flavour_json, flavour, iface))
	}
	metadata_json := strings.Join(metadata, ", ")
	return fmt.Sprintf(release_json, addon.Name, addon.Version, zip_output_filename, metadata_json)
}

// writes a 'release.json' JSON string `release_json` to `addon_output_dir`,
// returning the final output path.
func write_release_json(release_json string, addon_output_dir string) string {
	release_json_output_path := filepath.Join(addon_output_dir, "release.json")
	err := os.WriteFile(release_json_output_path, []byte(release_json), os.FileMode(int(0644)))
	panicOnErr(err, "writing release.json to file")
	stderr("wrote: " + release_json_output_path)
	return release_json_output_path
}

// returns the most recent tag of the addon's git repository at
// `addon_output_dir` to be compared against the version returned by the API.
func fetch_addon_version(addon_output_dir string) string {
	rc, _stdout, _stderr := run_cmd("git describe --tags --abbrev=0", addon_output_dir)
	if rc != 0 {
		if strings.Contains(_stderr, "fatal: No names found, cannot describe anything.") ||
			strings.Contains(_stderr, "fatal: No tags can describe") {
			return "" // no tags, no worries
		}
		ensure(rc == 0, "failed to fetch latest tag: "+_stderr)
	}
	return _stdout
}

// tags the addon's git repository at `addon_output_dir` with `version`.
func tag_addon(version string, addon_output_dir string) {
	cmd_list := []string{
		fmt.Sprintf("git commit -m %s --allow-empty", version),
		"git tag " + version,
		"git push",
		"git push --tags",
	}
	run_all_cmd(cmd_list, addon_output_dir)
}

// resets the git repository for the given `addon` by deleting and re-cloning it.
// ensures no errant tags or weird repository state are present.
func fetch_repo(addon Addon, script_path string) {
	cmd_list := []string{
		fmt.Sprintf("rm -rf %s", addon.Slug),
		"git clone ssh://git@github.com/ogri-la/" + addon.Slug,
	}
	run_all_cmd(cmd_list, script_path)
}

func guess_media_type(path string) string {
	mime, present := map[string]string{
		".zip":  "application/zip",
		".json": "application/json",
	}[strings.ToLower(filepath.Ext(path))]
	ensure(present, "failed to guess mime for given path: "+path)
	return mime
}

func create_tag_release(addon Addon, token string, asset_list []string) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// https://docs.github.com/en/rest/releases/releases?apiVersion=2022-11-28#create-a-release
	release := github.RepositoryRelease{
		TagName:    github.String(addon.Version),
		MakeLatest: github.String("true"),
	}
	release_result, _, err := client.Repositories.CreateRelease(ctx, "ogri-la", addon.Slug, &release)
	panicOnErr(err, "creating a Github release")
	for _, asset_path := range asset_list {
		upload_opts := github.UploadOptions{
			Name:      filepath.Base(asset_path),
			Label:     filepath.Base(asset_path),
			MediaType: guess_media_type(asset_path),
		}
		fh, err := os.Open(asset_path)
		panicOnErr(err, "opening asset: "+asset_path)
		_, _, err = client.Repositories.UploadReleaseAsset(ctx, "ogri-la", addon.Slug, release_result.GetID(), &upload_opts, fh)
		panicOnErr(err, "uploading asset: "+asset_path)
	}
}

// pulls a Github personal access token (PAT) out of an envvar `GITHUB_TOKEN`.
func github_token() string {
	token, present := os.LookupEnv("GITHUB_TOKEN")
	ensure(present, "envvar GITHUB_TOKEN not set.")
	return token
}

func mirror(app IMirror, token string) {
	script_path, err := os.Getwd()
	panicOnErr(err, "fetching the current working directory")
	for _, addon := range app.fetch_addon_list() {
		fetch_repo(addon, script_path)

		// "/path/to/output/dir/elvui/"
		addon_output_dir, err := filepath.Abs(addon.Slug)
		panicOnErr(err, "creating an absolute path for addon's output")

		current_version := fetch_addon_version(addon_output_dir)
		latest_version := addon.Version
		if current_version == latest_version {
			stderr(fmt.Sprintf("%s == %s, skipping", current_version, latest_version))
			continue
		}
		stderr(fmt.Sprintf("update detected for %s: '%s' => '%s'", addon.Name, current_version, latest_version))
		zip_output_path := app.download_addon(addon, addon_output_dir)

		zip_output_filename := filepath.Base(zip_output_path) // "elvui--3.33.zip"
		release_json := gen_release_json(addon, zip_output_filename)
		release_json_path := write_release_json(release_json, addon_output_dir)

		tag_addon(addon.Version, addon_output_dir)
		create_tag_release(addon, token, []string{zip_output_path, release_json_path})
	}
}

func main() {
	mirror(TukuiMirror{}, github_token())
}
