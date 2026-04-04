FROM eclipse-temurin:17-jdk-jammy

ARG GO_VERSION=1.24.3
ARG ANDROID_CMDLINE_TOOLS=13114758
ARG ANDROID_NDK_VERSION=29.0.14206865
ARG ANDROID_PLATFORM=android-36
ARG ANDROID_BUILD_TOOLS=36.0.0

ENV DEBIAN_FRONTEND=noninteractive \
    ANDROID_SDK_ROOT=/opt/android-sdk \
    ANDROID_HOME=/opt/android-sdk \
    ANDROID_NDK_HOME=/opt/android-sdk/ndk/${ANDROID_NDK_VERSION} \
    GRADLE_USER_HOME=/opt/gradle \
    PATH=/usr/local/go/bin:/root/go/bin:/opt/android-sdk/cmdline-tools/latest/bin:/opt/android-sdk/platform-tools:${PATH} \
    WINEPREFIX=/root/.wine

RUN dpkg --add-architecture i386 \
    && apt update \
    && apt install -y --no-install-recommends --no-install-suggests \
    bash build-essential ca-certificates curl git \
    libc6-dev pkg-config python3 rsync \
    gcc-multilib g++-multilib \
    unzip wget zip gnupg \
    wine64 wine32 wine mono-complete \
    && apt clean \
    && rm -rf /var/lib/apt/lists/*

RUN wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz

RUN mkdir -p ${ANDROID_SDK_ROOT}/cmdline-tools /workspace /opt/gradle \
    && mkdir -p /root/Library/Android \
    && ln -s ${ANDROID_SDK_ROOT} /root/Library/Android/sdk \
    && wget -q https://dl.google.com/android/repository/commandlinetools-linux-${ANDROID_CMDLINE_TOOLS}_latest.zip \
    -O /tmp/cmdline-tools.zip \
    && unzip -q /tmp/cmdline-tools.zip -d ${ANDROID_SDK_ROOT}/cmdline-tools \
    && mv ${ANDROID_SDK_ROOT}/cmdline-tools/cmdline-tools ${ANDROID_SDK_ROOT}/cmdline-tools/latest \
    && rm /tmp/cmdline-tools.zip \
    && yes | sdkmanager --licenses >/dev/null \
    && sdkmanager \
    "platform-tools" \
    "platforms;${ANDROID_PLATFORM}" \
    "build-tools;${ANDROID_BUILD_TOOLS}" \
    "ndk;${ANDROID_NDK_VERSION}" \
    && rm -rf ${ANDROID_SDK_ROOT}/ndk/${ANDROID_NDK_VERSION}/simpleperf \
    && rm -rf ${ANDROID_SDK_ROOT}/ndk/${ANDROID_NDK_VERSION}/shader-tools \
    && rm -rf ${ANDROID_SDK_ROOT}/ndk/${ANDROID_NDK_VERSION}/sources/third_party \
    && rm -rf ${ANDROID_SDK_ROOT}/.downloadIntermediates \
    && rm -rf ${ANDROID_SDK_ROOT}/.temp

RUN go install golang.org/x/mobile/cmd/gomobile@latest \
    && go install golang.org/x/mobile/cmd/gobind@latest \
    && go install github.com/konoui/lipo@latest \
    && go clean -cache -modcache -testcache \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
    | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_22.x nodistro main" \
    > /etc/apt/sources.list.d/nodesource.list \
    && apt update \
    && apt install -y --no-install-recommends --no-install-suggests nodejs \
    && apt clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace