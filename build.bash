#!/bin/bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)
readonly script_dir
readonly cache_volume=go-cache-nblog
readonly golang=docker.io/library/golang:1.20.2-alpine

readonly cache_path=/go-cache

args=$(getopt --options uc --longoptions update,clear --name "$(basename "$0")" -- "$@")
eval set -- "${args}"

update=false
while :; do
    case "$1" in
        -u | --update)
            readonly update=true
            ;;
        -c | --clear)
            # status 1 means a volume didn't exist, which is fine.
            podman volume rm "${cache_volume}" || test $? = 1
            exit
            ;;
        --)
            shift
            break
            ;;
    esac
    shift
done
readonly update

g() {
    local args
    args=(
        --interactive
        --rm
        --volume "${script_dir}:/src:rw"
        --volume "${cache_volume}:${cache_path}:rw"
        --env GOBIN="${cache_path}/bin"
        --env GOCACHE="${cache_path}/go"
        --env GOMODCACHE="${cache_path}/mod"
        --env CGO_ENABLED=0
        --label role=builder
        --workdir /src
        "${golang}"
    )
    (set -x; podman run "${args[@]}" "$@")
}

volume_args=(
    --label app=vtasloh
    --label role=cache
)

if ! podman volume exists "${cache_volume}"; then
    podman volume create "${volume_args[@]}" "${cache_volume}"
fi

g sh -x <<END
set -euo pipefail
if ${update}; then
    go get -u
fi
go run mage.go check
END

# vim: et sw=4 ts=4
