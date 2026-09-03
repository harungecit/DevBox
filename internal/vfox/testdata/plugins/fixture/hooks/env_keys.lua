function PLUGIN:EnvKeys(ctx)
    local mainPath = ctx.path
    local sep = "/"
    if RUNTIME.osType == "windows" then sep = "\\" end
    local helper = ctx.sdkInfo["helper"]
    local keys = {
        { key = "PATH", value = mainPath .. sep .. "bin" },
        { key = "FIXTURE_HOME", value = mainPath },
        { key = "FIXTURE_VERSION", value = ctx.main.version },
    }
    if helper then
        table.insert(keys, { key = "PATH", value = helper.path })
    end
    return keys
end
