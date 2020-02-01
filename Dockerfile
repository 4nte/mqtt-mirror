FROM scratch

COPY mqtt-mirror /

ENV SOURCE ""
ENV TARGET ""
ENV TOPIC_FILTER "#"

CMD /mqtt-mirror $SOURCE $TARGET --verbose --topic_filter $TOPIC_FILTER
