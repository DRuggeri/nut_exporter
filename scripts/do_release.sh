#!/bin/bash -e

OSs=("darwin" "linux" "windows")
ARCHs=("386" "amd64" "arm")

#Get into the right directory
cd $(dirname $0)

#Add this directory to PATH
export PATH="$PATH:`pwd`"

#Parse command line params
CONFIG=$@
for line in $CONFIG; do
  eval "$line"
done

if [[ -z "$github_api_token" ]];then
  if [[ -f ~/github_api_token ]];then
    github_api_token=$(cat ~/github_api_token)
  elif [[ -f github_api_token ]];then
    github_api_token=$(cat github_api_token)
  fi
fi

if [[ -z "$owner" ]];then
  owner="DRuggeri"
fi

if [[ -z "$repo" ]];then
  repo="nut_exporter"
fi

if [[ -z "$github_api_token" || -z "$owner" || -z "$repo" || -z "$tag" ]];then
  echo "USAGE: $0 github_api_token=TOKEN owner=someone repo=somerepo tag=vX.Y.Z"
  exit 1
fi

if [[ "$tag" != v* ]];then
  tag="v$tag"
fi

#Verify testing is good
./test.sh

#Build for all architectures we want
ARTIFACTS=()
#for GOOS in darwin linux windows netbsd openbsd solaris;do
echo "Building..."
for GOOS in "${OSs[@]}";do
  for GOARCH in "${ARCHs[@]}";do
    #An exception case... targeting Raspberry Pi Linux, mostly...
    if [[ "$GOARCH" == "arm" && "$GOOS" != "linux" ]];then continue; fi

    export GOOS GOARCH

    OUT_FILE="nut_exporter-$tag-$GOOS-$GOARCH"
    echo "  $OUT_FILE"
    go build -o "$OUT_FILE" ../
    ARTIFACTS+=("$OUT_FILE")
  done
done
export GOOS=""
export GOARCH=""

#Create the release so we can add our files
./create-github-release.sh github_api_token=$github_api_token owner=$owner repo=$repo tag=$tag draft=false

#Upload all of the files to the release
for FILE in "${ARTIFACTS[@]}";do
  ./upload-github-release-asset.sh github_api_token=$github_api_token owner=$owner repo=$repo tag=$tag filename="$FILE"
done

echo "Cleaning up..."
rm -f release_info.md
for GOOS in "${OSs[@]}";do
  for GOARCH in "${ARCHs[@]}";do
    rm -f "nut_exporter-$tag-$GOOS-$GOARCH"
  done
done
