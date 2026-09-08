#!/bin/sh
# Run sprue locally with the values in .env. Use: ./start.sh
set -e
cd "$(dirname "$0")"
if [ ! -f .env ]; then
	echo "No .env file. Copy .env.example to .env and fill it in." >&2
	exit 1
fi
set -a
. ./.env
set +a
cd platform
go build -o sprue .
echo "sprue on http://localhost:${PORT:-8080}"
exec ./sprue
