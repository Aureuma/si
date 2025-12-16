package main

import (
    "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
    pulumi.Run(func(ctx *pulumi.Context) error {
        ctx.Export("status", pulumi.String("pulumi-stack-ready"))
        // TODO: add real infra resources (DNS, SSL, networks, services)
        return nil
    })
}
