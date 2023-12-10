# nqk(d)

Not Quite Kubernetes

This project was born out of me experimenting with k8s (or specifically k3s). I liked the automation and how everything
was setup declaratively but from a single node machine with exclusively local storage, it didn't make sense to maintain
an entire k3s setup. Instead, I've replicated the features I like in a simple runtime that lets me declaratively define
everything in a docker compose file with some added niceties around volume mounts and networking. Specifically this
integrates nicely with nginx to automatically update the routing based on labels whenever the container details change.

## Setup

Setup can be done with the install command, this will perform the following checks and actions:

1. Any existing nqk services are stopped
2. The version you are trying to install is > the currently installed version (if present)
3. Docker is installed
4. Docker compose is installed
5. Docker is functional (can list containers)
6. That systemd files are installed

By the end of it, you should have the nqkd-apply service installed pointing to an installed nqk runtime! When
installing, you should specify a set of paths in which configuration files will be found.

## Usage

When the daemon apply service is running, it will periodically the file tree of the paths specified and find any yaml
files and try to parse them as docker compose files, applying them if needed. You can monitor the progress by using
the `nqk cli status` command, or force it to update with the `nqk cli apply` command.

To generate bindings, you can export them in json for use in any program (`nqk binding json`) or directly write nginx
config files (`nqk binding nginx`).

### Labelling

Exposing bindings is controlled through `labels` on each container. The following labels and their purposes are
supported. In all lines, where `.$port` is specified, it can be completely ommitted to act as a global flag (
ie `org.xiomi.nqkd.domain` will apply to everything, and `org.xiomi.nqkd.1234.domain` will only apply to port 1234)

| Label                                   | Purpose                                                                                                 |
|-----------------------------------------|---------------------------------------------------------------------------------------------------------|
| `org.xiomi.nqkd.$port.domain`           | The domain for which this service should be attached (ie `server_name` on nginx)                        |
| `org.xiomi.nqkd.$port.http.nonstandard` | This uses nonstandard ports for HTTP traffic and should not be mapped to `80`/`443`                     |
| `org.xiomi.nqkd.$port.ssl`              | This port should be exposed with ssl (ie `ssl_certificate`, `ssl_protocols`, or `ssl_ciphers` on nginx) |
| `org.xiomi.nqkd.$port.bind`             | The ports to bind to on the host                                                                        |
| `org.xiomi.nqkd.$port.type`             | The port type (ie http/tcp/udp)                                                                         |
| `org.xiomi.nqkd.$port.port.override`    | If using http-nonstandard port, this is the port that should be used instead                            |
| `org.xiomi.nqkd.$port.hide`             | **Port cannot be omitted**: don't expose this port at all through nginx                                 |