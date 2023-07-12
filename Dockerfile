FROM busybox

COPY mqtt-mirror /bin/mqtt-mirror
RUN chmod +x /bin/mqtt-mirror
ENTRYPOINT ["/bin/mqtt-mirror" ]