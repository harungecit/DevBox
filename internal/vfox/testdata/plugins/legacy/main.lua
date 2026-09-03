PLUGIN = {
    name = "legacy",
    version = "0.0.1",
    description = "single-file plugin layout",
}

function PLUGIN:Available(ctx)
    return { { version = "1.0.0" } }
end

function PLUGIN:PreInstall(ctx)
    return { version = ctx.version, url = "" }
end

function PLUGIN:EnvKeys(ctx)
    return { { key = "PATH", value = ctx.path } }
end
