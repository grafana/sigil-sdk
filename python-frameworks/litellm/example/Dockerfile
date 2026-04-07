FROM ghcr.io/berriai/litellm:v1.82.3-stable.patch.2

COPY sdks/python /tmp/sigil-sdk
COPY sdks/python-frameworks/litellm /tmp/sigil-sdk-litellm

RUN pip install --no-cache-dir /tmp/sigil-sdk /tmp/sigil-sdk-litellm && \
    rm -rf /tmp/sigil-sdk /tmp/sigil-sdk-litellm
