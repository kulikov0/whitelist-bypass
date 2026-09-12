FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      iproute2 iptables ncat ca-certificates procps \
 && rm -rf /var/lib/apt/lists/*

COPY work /work
RUN chmod +x /work/headless/*/headless-* \
             /work/headless/tests/*.sh \
             /work/headless/tests/stand/*.sh

CMD ["sleep", "infinity"]
