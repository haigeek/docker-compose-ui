# =============================================================================
# Runtime image
# Binary path: dist/compose-ui (prepared by deploy.sh before build)
# Run: ./deploy.sh (handles multi-arch build automatically)
# =============================================================================
FROM alpine:3.21

RUN apk add --no-cache docker-cli

WORKDIR /app

COPY dist/compose-ui ./compose-ui

EXPOSE 8227

ENV COMPOSE_UI_ADDR=:8227
ENV COMPOSE_UI_BASIC_AUTH_USER=admin
ENV COMPOSE_UI_BASIC_AUTH_PASS=admin
ENV COMPOSE_UI_REDEPLOY_TIMEOUT=120s

ENTRYPOINT ["./compose-ui"]
