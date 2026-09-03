local util = {}

function util.versions()
    return {
        { version = "2.0.0", note = "stable" },
        { version = "2.1.0-rc1", note = "" },
        { version = "1.9.0", note = "LTS", addition = { { name = "helper", version = "9" } } },
    }
end

return util
