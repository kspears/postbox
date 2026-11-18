# Test-stack postfix: Debian's package, with main.cf and master.cf mounted at run time. The
# init service runs on this image too, which is why openssl is here.
FROM debian:trixie-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2

# The postfix postinst writes a throwaway main.cf from debconf answers; the mounted one wins.
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      postfix ca-certificates openssl \
 && apt-get clean

# `postfix check` creates the queue subdirectories in a fresh volume and repairs owners and
# modes. exec so postfix is PID 1 and receives SIGTERM for a graceful stop.
ENTRYPOINT ["sh", "-c", "postfix check && exec postfix start-fg"]
