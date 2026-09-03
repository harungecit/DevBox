function PLUGIN:ParseLegacyFile(ctx)
    local installed = ctx:getInstalledVersions()
    local f = io.open(ctx.filepath, "r")
    local wanted = f:read("*l")
    f:close()
    wanted = wanted:gsub("%s+", "")
    if wanted == "installed" then
        return { version = installed[1] }
    end
    return { version = wanted }
end
