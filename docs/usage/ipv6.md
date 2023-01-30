# IPv6 in Gardener Clusters

> 🚧 IPv6 networking is currently under development.

## IPv6 Single-Stack Networking

[GEP-21](../proposals/21-ipv6-singlestack-local.md) proposes IPv6 Single-Stack Support in the local Gardener environment.
This documentation will be enhanced while implementing GEP-21, see [gardener/gardener#7051](https://github.com/gardener/gardener/issues/7051).

To use IPv6 single-stack networking, the [feature gate](../deployment/feature_gates.md) `IPv6SingleStack` must be enabled on gardener-apiserver.

## Development Setup

Developing or testing IPv6-related features requires a Linux machine (docker only supports IPv6 on Linux) and native IPv6 connectivity to the internet.
If you're on a different OS or don't have IPv6 connectivity in your office environment or via your home ISP, make sure to check out [gardener-community/dev-box-gcp](https://github.com/gardener-community/dev-box-gcp), which allows you to circumvent these limitations.