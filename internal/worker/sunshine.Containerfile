# Fixed offline adaptation of infrastructure/gaming/Dockerfile, contract v1.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
USER root
COPY sunshine.deb /tmp/workstation-sunshine.deb
COPY apt/lists /var/lib/apt/lists
COPY apt/archives /var/cache/apt/archives
COPY infrastructure/gaming/trixie-backports.sources /etc/apt/sources.list.d/workstation-backports.sources
RUN printf '%s  %s\n' c87f226920ad83055a898be1f0c7540307593e92d8b0baf1f076909758db8ca0 /tmp/workstation-sunshine.deb | sha256sum -c - \
    && DEBIAN_FRONTEND=noninteractive apt-get --no-download install -y --no-install-recommends -t trixie-backports \
       /tmp/workstation-sunshine.deb jq vainfo ffmpeg vulkan-tools \
       mesa-vulkan-drivers:amd64=26.1.2-1~bpo13+1 mesa-vulkan-drivers:i386=26.1.2-1~bpo13+1 \
       mesa-libgallium:amd64=26.1.2-1~bpo13+1 mesa-libgallium:i386=26.1.2-1~bpo13+1 \
       libgl1-mesa-dri:amd64=26.1.2-1~bpo13+1 libgl1-mesa-dri:i386=26.1.2-1~bpo13+1 \
       libegl-mesa0:amd64=26.1.2-1~bpo13+1 libegl-mesa0:i386=26.1.2-1~bpo13+1 \
       libglx-mesa0:amd64=26.1.2-1~bpo13+1 libglx-mesa0:i386=26.1.2-1~bpo13+1 \
       libgbm1:amd64=26.1.2-1~bpo13+1 libgbm1:i386=26.1.2-1~bpo13+1 \
    && mkdir -p /usr/lib/workstation /usr/share/workstation \
    && dpkg-query -W '-f=${binary:Package}\t${Version}\n' > /usr/share/workstation/gaming-packages.tsv \
    && mv /usr/bin/sunshine /usr/lib/workstation/sunshine.real \
    && if getcap /usr/lib/workstation/sunshine.real | grep -q .; then setcap -r /usr/lib/workstation/sunshine.real; fi \
    && rm /tmp/workstation-sunshine.deb
COPY bin /opt/workstation/bin
COPY lib /opt/workstation/lib
COPY templates /opt/workstation/templates
COPY versions.lock /opt/workstation/versions.lock
RUN chmod -R a+rX /opt/workstation
COPY --chmod=755 infrastructure/gaming/sunshine-wrapper.sh /usr/bin/sunshine
COPY --chmod=755 infrastructure/gaming/60-configure_gpu_driver.sh /etc/cont-init.d/60-configure_gpu_driver.sh
LABEL workstation.ai.qualification="pending-display-input-and-security"
