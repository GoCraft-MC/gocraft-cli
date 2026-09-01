# gocraft-cli

Builds and validates plugin bundles for the
[GoCraft](https://github.com/GoCraft-MC/GoCraft) server.

```sh
go install github.com/GoCraft-MC/gocraft-cli@latest
```

```sh
gocraft-cli validate ./my-plugin              # check plugin.toml
gocraft-cli build -o my-plugin.gcpkg ./my-plugin
```

## Why it is not part of the server

A plugin author compiles; they never run a server. Installing one to build the
other would be absurd, and a build tool that *could* reach into a server's
internals would eventually do it.

Here it cannot: the server is not on this module's graph. The whole dependency
list is `gocraft-abi` — the wire types, the command tree, and the `.gcpkg`
format — plus protobuf and a TOML parser underneath them. The rule is enforced
by the compiler rather than by review.

## What it does not reimplement

The bundle format and the command tree come from `gocraft-abi`, and the server
reads them with the same code. That is the property worth the split: a tree this
refuses is a tree the server would have refused, so the failure lands on the
machine that has the source rather than on someone's server at load time.

A second implementation here would agree on the day it was written and drift
quietly afterwards, which is the failure the shared contract exists to prevent.

## Versioning

It releases on its own clock. A plugin author has no reason to update their
build tool because the server fixed something in its world generator, and the
Gradle plugin pins a version of this rather than a version of the server.
