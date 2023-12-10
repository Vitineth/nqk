# nqk(d)

Not Quite Kubernetes

This project was born out of me experimenting with k8s (or specifically k3s). I liked the automation and how everything
was setup declaratively but from a single node machine with exclusively local storage, it didn't make sense to maintain
an entire k3s setup. Instead, I've replicated the features I like in a simple runtime that lets me declaratively define
everything in a docker compose file with some added niceties around volume mounts and networking. Specifically this
integrates nicely with nginx to automatically update the routing based on labels whenever the container details change.

## Setup

> [!NOTE]
> Coming soon

## Usage

> [!NOTE]
> Coming soon