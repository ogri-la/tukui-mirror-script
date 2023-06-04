package main

import (
	"bytes"
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
	Id        int      `json:"id"`   // -1
	Slug      string   `json:"slug"` // "elvui"
	Name      string   `json:"name"` // "ElvUI"
	Url       string   `json:"url"`
	Version   string   `json:"version"`
	PatchList []string `json:"patch"`
}

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

func fetch_addon_list() []Addon {
	url := "https://api.tukui.org/v1/addons"
	resp, err := http.Get(url)
	panicOnErr(err, "fetching addon list")
	defer resp.Body.Close()
	ensure(resp.StatusCode == 200, "non-200 response fetching addon list")

	body_bytes, err := ioutil.ReadAll(resp.Body)
	panicOnErr(err, "reading response body into a byte array")

	addon_list := []Addon{}
	err = json.Unmarshal(body_bytes, &addon_list)
	panicOnErr(err, "deserialising response bytes into a list of addon structs")

	return addon_list
}

// returns `true` if file at `path` exists.
func file_exists(path string) bool {
	_, err := os.Stat(path)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

// downloads zip file at `url` to file, returning the output path and panicking on any errors.
func download_addon(addon Addon, output_path string) string {
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

// 10.1.0 => mainline, 1.14.3 => classic, etc
func patch_to_flavour(patch string) string {
	prefix := patch[:2] // "1.14.3" => "1."
	if prefix == "1." {
		return "classic"
	}
	if prefix == "2." {
		return "classic-tbc"
	}
	if prefix == "3." {
		return "classic-wotlk"
	}
	return "mainline"
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

func gen_release_json(addon Addon, addon_output_path string, zip_output_filename string) string {
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

func write_release_json(release_json string, addon_output_dir string) string {
	release_json_output_path := filepath.Join(addon_output_dir, "release.json")
	err := os.WriteFile(release_json_output_path, []byte(release_json), os.FileMode(int(0644)))
	panicOnErr(err, "writing release.json to file")
	stderr("wrote: " + release_json_output_path)
	return release_json_output_path
}

// run a shell command, returning the return code, stdout and stderr.
func sh(command string, path string) (int, string, string) {
	cmd_bits := strings.Split(command, " ")
	cmd := exec.Command(cmd_bits[0], cmd_bits[1:]...)
	cmd.Dir = path
	var _stdout bytes.Buffer
	cmd.Stdout = &_stdout
	var _stderr bytes.Buffer
	cmd.Stderr = &_stderr
	rc := 0
	err := cmd.Run()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if ok {
			rc = exitError.ExitCode()
		}
	}
	return rc, _stdout.String(), _stderr.String()
}

func run_all(command_list []string, cwd string) {
	for _, cmd := range command_list {
		rc, _, _stderr := sh(cmd, cwd)
		ensure(rc == 0, fmt.Sprintf("command '%s' failed: %s", cmd, _stderr))
	}
}

func fetch_addon_version(addon_output_dir string) string {
	rc, _stdout, _stderr := sh("git describe --tags --abbrev=0", addon_output_dir)
	if rc != 0 {
		if _stderr == "fatal: No names found, cannot describe anything.\n" {
			return "" // no tags, no worries
		}
		ensure(rc == 0, "failed to fetch latest tag: "+_stderr)
	}
	return _stdout
}

func tag_addon(version string, addon_output_dir string) {
	cmd_list := []string{
		fmt.Sprintf("git commit -m %s --allow-empty", version),
		"git tag " + version,
		"git push",
		"git push --tags",
	}
	run_all(cmd_list, addon_output_dir)
}

func fetch_repos(script_path string) {
	cmd_list := []string{
		"rm -rf tukui",
		"git clone ssh://git@github.com/ogri-la/tukui",
		"rm -rf elvui",
		"git clone ssh://git@github.com/ogri-la/elvui",
	}
	run_all(cmd_list, script_path)
}

func guess_media_type(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		return "application/zip"
	}
	if ext == ".json" {
		return "application/json"
	}
	panic("failed to guess mime for given path: " + path)
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
	release_result, _, err := client.Repositories.CreateRelease(ctx, "ogri-la", addon.Name, &release)
	panicOnErr(err, "creating a Github release")

	for _, asset_path := range asset_list {
		upload_opts := github.UploadOptions{
			Name:      filepath.Base(asset_path),
			Label:     filepath.Base(asset_path),
			MediaType: guess_media_type(asset_path),
		}
		fh, err := os.Open(asset_path)
		panicOnErr(err, "failed to open asset")
		client.Repositories.UploadReleaseAsset(ctx, "ogri-la", addon.Name, release_result.GetID(), &upload_opts, fh)
	}
}

// pulls a Github personal access token (PAT) out of an envvar `GITHUB_TOKEN`
// panics if token does not exist.
func github_token() string {
	token, present := os.LookupEnv("GITHUB_TOKEN")
	ensure(present, "envvar GITHUB_TOKEN not set.")
	return token
}

func main() {
	script_path, err := os.Getwd()
	panicOnErr(err, "fetching the current working directory")

	token := github_token()

	fetch_repos(script_path)

	for _, addon := range fetch_addon_list() {
		// "/path/to/output/dir/elvui/"
		addon_output_dir, err := filepath.Abs(addon.Slug)
		panicOnErr(err, "creating an absolute path for addon's output")

		current_version := fetch_addon_version(addon_output_dir)
		latest_version := addon.Version
		if current_version == latest_version {
			continue
		}
		stderr(fmt.Sprintf("update detected for %s: '%s' => '%s'", addon.Name, current_version, latest_version))
		zip_output_path := download_addon(addon, addon_output_dir)

		zip_output_filename := filepath.Base(zip_output_path) // "elvui--3.33.zip"
		release_json := gen_release_json(addon, addon_output_dir, zip_output_filename)
		release_json_path := write_release_json(release_json, addon_output_dir)

		tag_addon(addon.Version, addon_output_dir)
		create_tag_release(addon, token, []string{zip_output_filename, release_json_path})
	}
}