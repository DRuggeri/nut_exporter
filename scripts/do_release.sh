#!/bin/bash -e


usage (){
echo "$0 - Tag and prepare a release

USAGE: $0 (major|minor|patch|vX.Y.Z)

The argument may be one of:
major  - Increments the current major version and performs the release
minor  - Increments the current minor version and preforms the release
patch  - Increments the current patch version and preforms the release
vX.Y.Z - Sets the tag to the value of vX.Y.Z where X=major, Y=minor, and Z=patch
"
  exit 1
}

if [ -z "$1" -o -n "$2" ];then
  usage
fi

TAG=`git describe --tags --abbrev=0`
VERSION="${TAG#[vV]}"
MAJOR="${VERSION%%\.*}"
MINOR="${VERSION#*.}"
MINOR="${MINOR%.*}"
PATCH="${VERSION##*.}"
echo "Current tag: v$MAJOR.$MINOR.$PATCH"

#Determine what the user wanted
case $1 in
  major)
    MAJOR=$((MAJOR+1))
    MINOR=0
    PATCH=0
    TAG="v$MAJOR.$MINOR.$PATCH"
    ;;
  minor)
    MINOR=$((MINOR+1))
    PATCH=0
    TAG="v$MAJOR.$MINOR.$PATCH"
    ;;
  patch)
    PATCH=$((PATCH+1))
    TAG="v$MAJOR.$MINOR.$PATCH"
    ;;
  v*.*.*)
    TAG="$1"
    ;;
  *.*.*)
    TAG="v$1"
    ;;
  *)
    usage
    ;;
esac

echo "New tag: $TAG"

#Get into the right directory
cd $(dirname $0)/..

vi .release_info.md

git commit -m "Changes for $TAG" .release_info.md

git tag $TAG
git push origin
git push origin $TAG
