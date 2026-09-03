function PLUGIN:PreInstall(ctx)
    local version = ctx.version
    if version == "latest" then
        version = "2.0.0"
    end
    if version == "missing" then
        return {}
    end
    -- FIXTURE_ARCHIVE is injected by the test through the environment; the
    -- archive lives next to the plugin so a local path works.
    return {
        version = version,
        url = os.getenv("DEVBOX_FIXTURE_ARCHIVE") or "",
        note = "from fixture",
        sha256 = os.getenv("DEVBOX_FIXTURE_SHA256") or "",
        addition = {
            { name = "helper", version = "9", url = os.getenv("DEVBOX_FIXTURE_ADDITION") or "" },
        },
    }
end
