# syntax=docker/dockerfile:1
# Reproducible build environment for the Android control app:
# JDK 17 + Android SDK/NDK + Gradle + Go + gomobile.
#
#   docker build -f infrastructure/docker/android-build.Dockerfile -t agentmesh/android-build .
#   docker run --rm -v "$PWD:/src" -v agentmesh-gradle:/root/.gradle -v agentmesh-gomod:/root/go/pkg/mod \
#     agentmesh/android-build mobile/android-control/build.sh

FROM golang:1.26 AS go

FROM eclipse-temurin:17-jdk-jammy
ARG CMDLINE_TOOLS=11076708
ARG NDK_VERSION=27.2.12479018
ARG GRADLE_VERSION=8.11.1
ARG GOMOBILE_VERSION=v0.0.0-20260908204917-8b95e45f8d3e

ENV ANDROID_HOME=/opt/android-sdk \
    ANDROID_SDK_ROOT=/opt/android-sdk \
    ANDROID_NDK_HOME=/opt/android-sdk/ndk/${NDK_VERSION} \
    GOTOOLCHAIN=local \
    PATH=/usr/local/go/bin:/root/go/bin:/opt/android-sdk/cmdline-tools/latest/bin:/opt/android-sdk/platform-tools:/opt/gradle/bin:$PATH

RUN apt-get update && apt-get install -y --no-install-recommends unzip wget git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p ${ANDROID_HOME}/cmdline-tools \
    && wget -q https://dl.google.com/android/repository/commandlinetools-linux-${CMDLINE_TOOLS}_latest.zip -O /tmp/tools.zip \
    && unzip -q /tmp/tools.zip -d ${ANDROID_HOME}/cmdline-tools \
    && mv ${ANDROID_HOME}/cmdline-tools/cmdline-tools ${ANDROID_HOME}/cmdline-tools/latest \
    && rm /tmp/tools.zip \
    && yes | sdkmanager --licenses > /dev/null \
    && sdkmanager --install "platform-tools" "platforms;android-35" "build-tools;35.0.0" "ndk;${NDK_VERSION}" > /dev/null

RUN wget -q https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip -O /tmp/gradle.zip \
    && unzip -q /tmp/gradle.zip -d /opt && mv /opt/gradle-${GRADLE_VERSION} /opt/gradle && rm /tmp/gradle.zip

COPY --from=go /usr/local/go /usr/local/go
RUN go install golang.org/x/mobile/cmd/gomobile@${GOMOBILE_VERSION} \
    && go install golang.org/x/mobile/cmd/gobind@${GOMOBILE_VERSION}

WORKDIR /src
