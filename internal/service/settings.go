package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"DevBox/internal/platform"
)

// This file implements the editable connection settings shown in the service
// info panel: user, password and (for Redis/Valkey) database count. Port
// changes go through ServiceManager.SetPort.

// DefaultUser is the account DevBox creates at install time.
func DefaultUser(name string) string {
	switch name {
	case "postgres":
		return "postgres"
	case "mysql", "mariadb":
		return "root"
	}
	return ""
}

// Credentials returns the current user/password DevBox connects with.
func Credentials(name string) (user, password string) {
	cfg := LoadServiceConfig(name)
	user = cfg.User
	if user == "" {
		user = DefaultUser(name)
	}
	return user, cfg.Password
}

// RedisDatabases returns the configured keyspace count (default 16).
func RedisDatabases(name string) int {
	if n := LoadServiceConfig(name).Databases; n > 0 {
		return n
	}
	return 16
}

// SetSetting applies one editable connection setting to a service and
// persists it. key: "user" | "password" | "databases".
func SetSetting(name, key, value string) error {
	mgr, ok := Registry[name]
	if !ok || !mgr.IsInstalled() {
		return fmt.Errorf("%s is not installed", name)
	}
	value = strings.TrimSpace(value)
	switch name {
	case "postgres":
		return setPostgresCredential(mgr, key, value)
	case "mysql", "mariadb":
		return setMySQLCredential(mgr, name, key, value)
	case "redis", "valkey":
		return setRedisSetting(mgr, name, key, value)
	}
	return fmt.Errorf("%s has no editable %s", name, key)
}

// ensureRunning starts the service if needed and waits for its port.
func ensureRunning(mgr ServiceManager) error {
	if mgr.Status() == StatusRunning {
		return nil
	}
	if err := mgr.Start(); err != nil {
		return err
	}
	for i := 0; i < 40; i++ {
		if IsPortInUse(mgr.Port()) {
			time.Sleep(500 * time.Millisecond) // let the server finish booting
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("%s did not come up on port %d", mgr.Name(), mgr.Port())
}

func runWithEnv(exe string, args []string, dir string, env ...string) (string, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	platform.SetProcessAttrs(cmd, false, true)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func sqlString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// ---- PostgreSQL ----

func setPostgresCredential(mgr ServiceManager, key, value string) error {
	cfg := LoadServiceConfig("postgres")
	curUser, curPass := Credentials("postgres")
	newUser, newPass := curUser, curPass
	switch key {
	case "user":
		if !identRe.MatchString(value) {
			return fmt.Errorf("invalid user name")
		}
		newUser = value
	case "password":
		newPass = value
	default:
		return fmt.Errorf("postgres has no editable %s", key)
	}

	if err := ensureRunning(mgr); err != nil {
		return err
	}
	base := serviceBaseDir("postgres")
	psql := filepath.Join(base, "bin", platform.BinaryName("psql"))
	port := strconv.Itoa(mgr.Port())
	run := func(sql string) error {
		out, err := runWithEnv(psql, []string{"-U", curUser, "-h", "127.0.0.1", "-p", port, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", sql},
			base, "PGPASSWORD="+curPass)
		if err != nil {
			return fmt.Errorf("psql: %s", out)
		}
		return nil
	}
	pw := "PASSWORD NULL"
	if newPass != "" {
		pw = "PASSWORD " + sqlString(newPass)
	}
	if newUser != curUser {
		// Create the role if missing (a superuser, like the default one), then set its password.
		run(fmt.Sprintf(`CREATE ROLE "%s" WITH LOGIN SUPERUSER`, newUser)) // ignore "already exists"
	}
	if err := run(fmt.Sprintf(`ALTER ROLE "%s" WITH LOGIN SUPERUSER %s`, newUser, pw)); err != nil {
		return err
	}

	// initdb defaults to "trust": a password is never asked. Switch the
	// loopback rules to scram when a password is set, back to trust when cleared.
	hba := filepath.Join(base, "data", "pg_hba.conf")
	if data, err := os.ReadFile(hba); err == nil {
		method := "trust"
		if newPass != "" {
			method = "scram-sha-256"
		}
		lines := strings.Split(string(data), "\n")
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			f := strings.Fields(t)
			if len(f) >= 4 && (f[0] == "host" || f[0] == "local") {
				f[len(f)-1] = method
				lines[i] = strings.Join(f, "\t")
			}
		}
		os.WriteFile(hba, []byte(strings.Join(lines, "\n")), 0644)
	}

	cfg.User, cfg.Password = newUser, newPass
	if err := SaveServiceConfig("postgres", cfg); err != nil {
		return err
	}
	return mgr.Restart()
}

// ---- MySQL / MariaDB ----

func mysqlClient(name string) string {
	bin := filepath.Join(serviceBaseDir(name), "bin")
	for _, c := range []string{"mariadb", "mysql"} {
		if name == "mysql" && c == "mariadb" {
			continue
		}
		p := filepath.Join(bin, platform.BinaryName(c))
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(bin, platform.BinaryName("mysql"))
}

func setMySQLCredential(mgr ServiceManager, name, key, value string) error {
	cfg := LoadServiceConfig(name)
	curUser, curPass := Credentials(name)
	newUser, newPass := curUser, curPass
	switch key {
	case "user":
		if !identRe.MatchString(value) {
			return fmt.Errorf("invalid user name")
		}
		newUser = value
	case "password":
		newPass = value
	default:
		return fmt.Errorf("%s has no editable %s", name, key)
	}
	if err := ensureRunning(mgr); err != nil {
		return err
	}
	base := serviceBaseDir(name)
	cli := mysqlClient(name)
	var sql []string
	for _, host := range []string{"localhost", "127.0.0.1"} {
		acct := fmt.Sprintf("'%s'@'%s'", newUser, host)
		if newUser != curUser || host == "127.0.0.1" {
			sql = append(sql, fmt.Sprintf("CREATE USER IF NOT EXISTS %s IDENTIFIED BY %s", acct, sqlString(newPass)))
			sql = append(sql, fmt.Sprintf("GRANT ALL PRIVILEGES ON *.* TO %s WITH GRANT OPTION", acct))
		}
		sql = append(sql, fmt.Sprintf("ALTER USER %s IDENTIFIED BY %s", acct, sqlString(newPass)))
	}
	sql = append(sql, "FLUSH PRIVILEGES")
	out, err := runWithEnv(cli, []string{"-u", curUser, "-h", "127.0.0.1", "-P", strconv.Itoa(mgr.Port()), "--protocol=tcp", "-e", strings.Join(sql, "; ")},
		base, "MYSQL_PWD="+curPass)
	if err != nil {
		return fmt.Errorf("%s: %s", filepath.Base(cli), out)
	}
	cfg.User, cfg.Password = newUser, newPass
	return SaveServiceConfig(name, cfg)
}

// mysqlAdminArgs returns the auth args mysqladmin needs for a graceful
// shutdown now that root may have a password.
func mysqlAdminEnv(name string) (args []string, env []string) {
	user, pass := Credentials(name)
	return []string{"-u", user, "-h", "127.0.0.1", "-P", strconv.Itoa(Registry[name].Port()), "--protocol=tcp"}, []string{"MYSQL_PWD=" + pass}
}

// ---- Redis / Valkey ----

func setRedisSetting(mgr ServiceManager, name, key, value string) error {
	cfg := LoadServiceConfig(name)
	switch key {
	case "password":
		cfg.Password = value
	case "databases":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 1024 {
			return fmt.Errorf("databases must be a number between 1 and 1024")
		}
		cfg.Databases = n
	default:
		return fmt.Errorf("%s has no editable %s", name, key)
	}
	if err := SaveServiceConfig(name, cfg); err != nil {
		return err
	}
	// Rewrite the config file from the saved settings (SetPort does the same).
	if err := mgr.SetPort(mgr.Port()); err != nil {
		return err
	}
	if mgr.Status() == StatusRunning {
		return mgr.Restart()
	}
	return nil
}

// redisExtraConfig renders the optional directives redis.conf/valkey.conf
// share: requirepass and databases.
func redisExtraConfig(name string) string {
	cfg := LoadServiceConfig(name)
	var b strings.Builder
	fmt.Fprintf(&b, "databases %d\n", RedisDatabases(name))
	if cfg.Password != "" {
		fmt.Fprintf(&b, "requirepass %s\n", sqlStringDoubleSafe(cfg.Password))
	}
	return b.String()
}

// sqlStringDoubleSafe quotes a redis.conf value (double quotes, backslash escapes).
func sqlStringDoubleSafe(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
