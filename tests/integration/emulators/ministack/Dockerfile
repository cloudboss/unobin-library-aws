ARG MINISTACK_IMAGE=ghcr.io/cloudboss/docker.io/ministackorg/ministack:1.5.43
FROM ${MINISTACK_IMAGE}
COPY patch_codebuild.py /usr/local/lib/ministack-patches/patch_codebuild.py
RUN python3 /usr/local/lib/ministack-patches/patch_codebuild.py
