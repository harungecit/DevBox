function PLUGIN:PostInstall(ctx)
    local root = ctx.rootPath
    local main = ctx.sdkInfo["fixture"]
    -- os.execute goes through DevBox's hidden-window runner
    local cmd
    if RUNTIME.osType == "windows" then
        cmd = "echo post-install > \"" .. main.path .. "\\post.txt\""
    else
        cmd = "echo post-install > '" .. main.path .. "/post.txt'"
    end
    local code = os.execute(cmd)
    if code ~= 0 then
        error("post install command failed: " .. tostring(code))
    end
    -- io.popen shim
    local f = io.popen("echo popen-works")
    local out = f:read("*a")
    f:close()
    if not out:find("popen-works", 1, true) then
        error("io.popen output missing: " .. tostring(out))
    end
    local marker = io.open(root .. "/post-ok", "w")
    marker:write(out)
    marker:close()
end
