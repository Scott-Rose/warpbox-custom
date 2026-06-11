#!/bin/sh
# Warpbox entrypoint — supports hot-swap dev deployment.
#
# If /data/warpbox-next exists (written by the dev-deploy script), replace the
# current binary before starting.  This allows rapid iterative development
# without rebuilding the Docker image: just restart the container after
# placing a new binary at /data/warpbox-next.
#
# For normal production use /data/warpbox-next never exists, so the
# if-block is a no-op and the original image binary runs unchanged.

set -e

if [ -f /data/warpbox-next ]; then
    echo "warpbox-entrypoint: upgrading binary from /data/warpbox-next"
    cp /data/warpbox-next /usr/local/bin/warpbox
    chmod 755 /usr/local/bin/warpbox
    rm /data/warpbox-next
    echo "warpbox-entrypoint: upgrade complete"
fi

exec warpbox --config /data/config.yml --db /data/warpbox.db