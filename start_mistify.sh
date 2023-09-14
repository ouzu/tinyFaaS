#!/usr/bin/env bash

trap 'kill $(jobs -p)' EXIT

TOML_FILES=($(find ./configs -maxdepth 1 -type f -name "*.toml" | sort -t'/' -k3,3))

run_mistify() {
    local toml_file="$1"
    echo "Running mistify for $toml_file"
    ./result/bin/mistify "$toml_file"
}

for toml_file in "${TOML_FILES[@]}"; do
    run_mistify "$toml_file" &
    sleep 0.5
done

wait
