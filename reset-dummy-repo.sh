#!/bin/bash
set -eux
repo=$1
rev=$2

[[ "$repo" == *-dummy ]]

rm -rf "$repo"
git clone "ssh://git@github.com/ogri-la/$repo"

(
    cd "$repo"
    git reset --hard "$rev"
    git push --force origin master
    git checkout master
    git tag -l | xargs -n 1 git push --delete origin || true
    git tag | xargs git tag -d
)
