# mqtt-mirror

![docker](https://img.shields.io/github/go-mod/go-version/4nte/mqtt-mirror)
![docker](https://img.shields.io/docker/pulls/antegulin/mqtt-mirror)
![version](https://img.shields.io/github/v/release/4nte/mqtt-mirror?sort=semver)
![license](https://img.shields.io/github/license/4nte/mqtt-mirror)


<p align="center">
  <img alt="mqtt-mirror diagram" src="https://i.imgur.com/EOGwXRf.png" height="150" />
  <h3 align="center">mqtt-mirror</h3>
  <p align="center">Fork MQTT traffic with no fuss, deploy in matter of seconds.</p>
</p>



---

Mqtt-mirror subscribes to a _source broker_ and publishes replicated messages to a _target broker_.  
Replicated messages preserve the _QoS_ and _Retain_ values of the original message.

## Get Started



**Homebrew**
```
brew tap 4nte/homebrew-tap
brew install mqtt-mirror
```

**Shell script**
```
curl -sfL https://raw.githubusercontent.com/4nte/mqtt-mirror/master/install.sh | sh
```

**Compile from source**
```
git clone https://github.com/4nte/mqtt-mirror
cd mqtt-mirror
go build
```
