#!/bin/sh
# Install in /etc/letsencrypt/renewal-hooks/deploy/moshu-nocf.sh (mode 700).
set -eu
[ "${RENEWED_LINEAGE:-}" = /etc/letsencrypt/live/moshu-nocf ] || exit 0
cert_dir=/opt/1panel/www/sites/nocf.moshu.cloud/ssl
install -m 644 "$RENEWED_LINEAGE/fullchain.pem" "$cert_dir/fullchain.pem.new"
install -m 600 "$RENEWED_LINEAGE/privkey.pem" "$cert_dir/privkey.pem.new"
mv "$cert_dir/fullchain.pem.new" "$cert_dir/fullchain.pem"
mv "$cert_dir/privkey.pem.new" "$cert_dir/privkey.pem"
docker exec 1Panel-openresty-ZmgO nginx -t
docker exec 1Panel-openresty-ZmgO nginx -s reload
