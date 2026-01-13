#!/bin/sh
# docker-entrypoint.sh
#
# Entrypoint wrapper for dnsweaver that loads Docker secrets into
# environment variables before starting the application.
#
# This is useful when you want to use YAML configuration with ${VAR}
# interpolation, where the variables are sourced from Docker secrets.
#
# Usage in Dockerfile:
#   COPY docker-entrypoint.sh /docker-entrypoint.sh
#   ENTRYPOINT ["/docker-entrypoint.sh"]
#   CMD ["dnsweaver"]
#
# Usage in docker-compose/swarm:
#   secrets:
#     - technitium_token
#   environment:
#     - DNSWEAVER_CONFIG=/config/config.yml
#
# Your config.yml can then use:
#   config:
#     token: ${TECHNITIUM_TOKEN}
#
# The script will read /run/secrets/technitium_token and export it as
# TECHNITIUM_TOKEN before dnsweaver starts.

set -e

# Load secrets from /run/secrets/ into environment variables
# Each file becomes an env var: technitium_token -> TECHNITIUM_TOKEN
load_secrets() {
    SECRETS_DIR="/run/secrets"

    if [ -d "$SECRETS_DIR" ]; then
        for secret_file in "$SECRETS_DIR"/*; do
            if [ -f "$secret_file" ]; then
                # Get filename and convert to uppercase env var name
                filename=$(basename "$secret_file")
                varname=$(echo "$filename" | tr '[:lower:]-' '[:upper:]_')

                # Read secret value (trim trailing newline)
                value=$(cat "$secret_file" | tr -d '\n')

                # Export as environment variable
                export "$varname"="$value"
                echo "Loaded secret: $varname"
            fi
        done
    fi
}

# Load Docker secrets
load_secrets

# Execute the main command
exec "$@"
