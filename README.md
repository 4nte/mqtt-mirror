# mqtt-mirror

![docker](https://img.shields.io/github/go-mod/go-version/4nte/mqtt-mirror)
![docker](https://img.shields.io/docker/pulls/antegulin/mqtt-mirror)
![version](https://img.shields.io/github/v/release/4nte/mqtt-mirror?sort=semver)
![license](https://img.shields.io/github/license/4nte/mqtt-mirror)


<p align="center">
  <img alt="mqtt-mirror diagram" src="https://i.imgur.com/EOGwXRf.png" height="150" />
  <h3 align="center">mqtt-mirror</h3>
<p align="center">Fork MQTT traffic with no fuss, deploy in seconds. Kubernetes ready.</p>


---

Mqtt-mirror subscribes to the _source broker_ and publishes replicated messages to the _target broker_.  
Replicated messages preserve the original _QoS_ and _Retain_ message options.  


All topics are mirrored by default, you can cherry pick topics to be mirrored by specifying topic filters. Standard MQTT wildcards `+` and `#` are available, [see wildcard spec](https://mosquitto.org/man/mqtt-7.html).

![Example usage](./img/demo.svg)

#### Should I use this in production?  
mqtt-mirror is not tested well enough to be relied upon for critical purposes. Until a stable 1.0 release, use with caution.

Take in consideration that outbound traffic will increase by the amount of inbound traffic.  
Use topic filters to prevent mirroring of unecessary messages.

mqtt-mirror is used in production at [spotsie.io](https://spotsie.io) ! :sparkles:

### 1.0 (GA) roadmap 
- [ ] Helm chart liveness probe
- [x] Integration test
- [ ] Stress test
- [ ] Expose Prometheus metrics

## Get started

Mqtt-mirror is available as a **standalone binary**, **docker image** and **helm chart**.

### Install

**Docker** :whale:
```
docker run antegulin/mqtt-mirror ./mqtt-mirror \
tcp://username:pass@source.xyz:1883 \
tcp://target.xyz:1883 \
--topic_filter=events,sensors/+/temperature/+,logs# \
--verbose
```

**Helm chart** :package:
```
helm repo add 4nte https://raw.githubusercontent.com/4nte/
helm install mqtt-mirror 4nte/mqtt-mirror \
--set mqtt.source=$SOURCE_BROKER \
--set mqtt.target=$TARGET_BROKER \
--set mqtt.topic_filter=foo,bar,device/+/ping \
```

**Homebrew** :beer:
```
brew tap 4nte/homebrew-tap
brew install mqtt-mirror
```

**Shell script** :clipboard:
```
curl -sfL https://raw.githubusercontent.com/4nte/mqtt-mirror/master/install.sh | sh
```


**Compile from source** :hammer:
```
# Clone it outside GO path
git clone https://github.com/4nte/mqtt-mirror
cd mqtt-mirror

# Get dependencies
go get ./..


# Build, duh.
go build -o mqtt-mirror

# Use it like there's no tomorrow
./mqtt-mirror --version
```

## Sponsors
![spotsie](https://spotsie.io/images/spotsie.svg)

## Development
If you like this project, please consider helping out. All contributions are welcome.
