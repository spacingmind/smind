#!/bin/bash
# Runs every image smoke test against $IMAGE (default smind:ci).
set -euo pipefail
cd "$(dirname "$0")"
status=0
for t in test_image_basics.sh test_image_chowns_root_owned_volume.sh test_image_state_survives_recreate.sh \
         test_image_relay_config_persists.sh test_image_login_through_published_port.sh; do
  echo "=== $t"
  if ! bash "./$t"; then status=1; echo "FAILED: $t" >&2; fi
done
exit "$status"
