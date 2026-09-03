function PLUGIN:PreUninstall(ctx)
    local target = os.getenv("DEVBOX_FIXTURE_UNINSTALL_MARKER") or (ctx.main.path .. "/../pre-uninstall-ran")
    local marker = io.open(target, "w")
    marker:write(ctx.main.version)
    marker:close()
end
