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
