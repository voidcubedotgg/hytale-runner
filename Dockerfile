# syntax=docker/dockerfile:1

# Build stage: compile the static runner binary.
FROM golang:1.26-bookworm AS build
WORKDIR /app
COPY . .
RUN go mod download && \
    CGO_ENABLED=0 go build -v -o hytale-runner .

# Runtime stage: the MicroVM image entrypoint.
#
# AWS Lambda MicroVM images must build on a Lambda-published managed base image
# (Amazon Linux 2023), supplied via the `base-image-arn` parameter at image
# creation. Override BASE_IMAGE with that ARN when building for Lambda; the
# default mirrors the AL2023 environment so the image builds locally too.
ARG BASE_IMAGE=public.ecr.aws/amazonlinux/amazonlinux:2023
FROM ${BASE_IMAGE} AS final

# Hytale runs on the JVM.
RUN dnf install -y java-21-amazon-corretto-headless && dnf clean all

COPY --from=build /app/hytale-runner /usr/local/bin/hytale-runner

# Immutable game bits from the build context. Provide HytaleServer.jar and
# Assets.zip at these paths (see --server-jar-path / --assets-path).
COPY HytaleServer.jar /hytale/HytaleServer.jar
COPY Assets.zip /hytale/Assets.zip

# Resident lifecycle-hook server. It exposes the run/suspend/resume/terminate
# hooks and only starts the game process when the run hook fires. Enable the
# /ready hook so Lambda snapshots the image once this server is listening.
EXPOSE 8080
ENTRYPOINT ["hytale-runner", "serve"]
