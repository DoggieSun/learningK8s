# learningK8s

This repository contains a small Go program that simulates how Kubernetes
pod workers process update and termination events. It is intended for
learning purposes and does not require an actual Kubernetes cluster.

## Prerequisites

- Go 1.23 or newer

## Running the example

Clone the repository and execute:

```bash
go run main.go
```

You should see each pod being updated several times before it is
terminated.

## Project structure

- **main.go** – Implementation of the minimal pod worker simulation.
- **go.mod** – Module definition.

Feel free to experiment with the code to better understand the workflow
of pod workers in Kubernetes.
