# Wazigate Tunnel

The Wazigate Tunnel is a reverse HTTP over MQTT proxy for the Waziup Cloud platform. You can use this service to access your gateway remotely.
Any incomming HTTP requests to this service will be forwarded to separate MQTT tunnel topics that are served by the gateways.

# Environment Variables

Name | Description | Default Value
--- | --- | ---
`WAZIUP_MQTT` | Cloud MQTT address. The port required. | `api.waziup.io:1883`
`WAZIUP_USERNAME` | Username used when connecting to MQTT. | `guest`
`WAZIUP_PASSWORD` | Password used when connecting to MQTT. | `guest`
`WAZIUP_ADDR` | Address this server listens to. | `:80` (default HTTP port)

# Docker Hub

This service is available at the docker hub as [waziup/wazigat-tunnel](https://hub.docker.com/r/waziup/wazigate-tunnel).

# How does it work?

This repository implements the cloud side of the tunnel. The counterpart is implemented by [wazigate-edge](https://github.com/Waziup/wazigate-edge/blob/v2/clouds/mqtt_sync.go#L166).

Prerequisite:

- Each Wazigate subscribes to `devices/{deviceID}/tunnel-down/+` via MQTT. The plus `+` wildcard matches any string. The cloud MQTT server must not allow any other Wazigate to that topic.

- The cloud service subscibes to `devices/+/tunnel-up/+` via MQTT. The plus `+` wildcard matches any string. The cloud MQTT server must not allow any subscriptions from clients to this topic.

The step-by-step procedure to access `index.html` from Wazigate `myWazigate`.

1. Open a website at the cloud tunnel service, e.g [https://remote.waziup.io/myWazigate/index.html](https://remote.waziup.io/myWazigate/index.html).
2. The cloud tunnel service will forward the request (the method, the path, the headers, the request body (if any)) to the `tunnel-down` topic: `devices/myWazigate/tunnel-down/425` with a random request ID `425`. The initial request is stalled!
3. If the Wazigate is online and connected to MQTT it will receive the request via it's subscription.
4. The Wazigate receives the request and performs the HTTP request locally. It calls the local webserver at port 80.
5. The result of that request is forwarded to the `tunnel-up` topic `devices/myWazigate/tunnel-up/425` using the same request ID `425`.
6. The cloud receives the response via it's subscription to `devices/+/tunnel-up/+`. It serves the response the request that was initially stalled.

What if ...

- ... the Wazigate is not connected / is offline?

     The cloud service will wait for a response on the `tunnel-up` topic for max. a few seconds. If no message is received, it will serve a generic "Gateway offline" message to the initial stalled request.


Keep in mind that ...

-  ... the cloud service maps request from [https://remote.waziup.io/myWazigate/index.html](https://remote.waziup.io/myWazigate/index.html) (at the cloud tunnel service) to [http://localhost/index.html](http://localhost/index.html) (on the Wazigate).

    All files at the Wazigate dashboard must be able to be served at the relative URLs at the cloud service!


