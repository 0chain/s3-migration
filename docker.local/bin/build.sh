#!/bin/sh
set -e

GIT_COMMIT=$(git rev-list -1 HEAD)
echo $GIT_COMMIT
echo "1> set DOCKER_IMAGE & DOCKER_BUILD"
if [ -z "$DOCKER_BUILD" ]; then  
    if [ "x86_64" != "$(uname -m)" ]; then
        #docker buildx use blobber_buildx || docker buildx create --name blobber_buildx --use
        DOCKER_BUILD="buildx build --platform linux/arm64"
    else
        DOCKER_BUILD="build"
    fi
fi

if [ -z "$DOCKER_IMAGE" ]; then  
    DOCKER_IMAGE="-t s3mgrt"
fi
echo "  DOCKER_BUILD=$DOCKER_BUILD"
echo "  DOCKER_IMAGE=$DOCKER_IMAGE"

echo ""
echo "2> docker build"
DOCKER_BUILDKIT=1 docker $DOCKER_BUILD --progress=plain -f docker.local/Dockerfile . $DOCKER_IMAGE