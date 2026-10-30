# shellcheck shell=sh
# serverpro-reboot finishes kernel and library updates with a reboot only when
# every running workload is declared to come back, and checks after boot that
# it did. serverpro renders the settings block above this body (see
# rebootgate.go), so the copy installed on a host and the copy doctor runs
# apply the same rules.
#
# Modes:
#   preflight  print each running workload that a reboot would drop
#   window     run by the timer: reboot only when a reboot is pending and the
#              preflight finds no gap
#   verify     run once at boot after a gated reboot: wait for the workloads
#              in the snapshot, start a missing one at most once, report
set -u
export LC_ALL=C
# The snapshot and the result name workloads; only root reads them.
umask 077
# A unit's environment or an operator's shell must not choose which
# systemctl, docker, or ss this root script runs.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

# syslog priorities that journald reads from PRIORITY=.
PRIORITY_ERR=3
PRIORITY_WARNING=4
PRIORITY_NOTICE=5
PRIORITY_INFO=6
# Wrong usage differs from a gap and from a read failure for any caller.
USAGE_EXIT_CODE=2
# Linux's default ip_local_port_range start, used only when /proc cannot say:
# a listener at or above it has a port the kernel picked, which is not stable
# across a reboot.
DEFAULT_EPHEMERAL_PORT_FLOOR=32768
EPHEMERAL_PORT_RANGE_PATH=/proc/sys/net/ipv4/ip_local_port_range
# The boot ID changes on every boot, which ties a snapshot and a result to
# the boot that wrote them.
BOOT_ID_PATH=/proc/sys/kernel/random/boot_id
# Docker's container scopes under the systemd and the cgroupfs cgroup
# drivers. Bootstrap turns on live-restore, so containers keep running
# while dockerd is stopped, and docker cannot list them then.
DOCKER_SCOPE_PROCS_GLOBS='/sys/fs/cgroup/system.slice/docker-*.scope/cgroup.procs /sys/fs/cgroup/docker/*/cgroup.procs'
# The system bus's standard activation directories on Ubuntu 24.04 (dbus
# 1.14's <standard_system_servicedirs/>), highest precedence first. The
# system bus loads only a file named after the bus name, so one test per
# directory finds the name's file.
DBUS_SYSTEM_SERVICE_DIRS='/usr/local/share/dbus-1/system-services /usr/share/dbus-1/system-services /lib/dbus-1/system-services'
# The activation file key that hands a bus name's activation to systemd. A
# file without it makes dbus-daemon run its Exec= line itself, inside
# dbus.service, so no service unit of its own comes back.
DBUS_SYSTEMD_SERVICE_KEY=SystemdService
# Records use the ASCII unit separator: read treats a tab as IFS
# whitespace and merges an empty field, such as a service with no
# TriggeredBy, into its neighbour, which would shift every later field.
FS_CHAR=$(printf '\037')
# The format names only what the gate needs: never the environment, the
# command line, or labels, which can hold secrets.
CONTAINER_FORMAT="{{.Name}}$FS_CHAR{{.HostConfig.RestartPolicy.Name}}$FS_CHAR{{.Config.Image}}$FS_CHAR{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}"
CONTAINER_STATE_FORMAT='{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'

mode=${1:-}
gaps=0

# log_event writes one journald entry with stable fields, so an operator can
# filter the gate's decisions with journalctl SERVERPRO_EVENT=... instead of
# parsing prose. Without journald the unit's stderr is still kept.
log_event() {
  message=$(printf '%s' "$4" | tr '\n' ' ')
  # journalctl's default view prints MESSAGE only, so it names the subject
  # too; SERVERPRO_SUBJECT keeps it filterable.
  [ "$3" = host ] || message="$3: $message"
  if ! printf 'MESSAGE=%s\nPRIORITY=%s\nSYSLOG_IDENTIFIER=%s\nSERVERPRO_EVENT=%s\nSERVERPRO_SUBJECT=%s\n' \
    "$message" "$1" "$LOG_TAG" "$2" "$3" | logger --journald 2>/dev/null; then
    printf '%s: %s %s: %s\n' "$LOG_TAG" "$2" "$3" "$message" >&2
  fi
}

# fail_closed stops before any reboot or restart: a host state the gate
# cannot read must never read as a host with no gaps.
fail_closed() {
  if [ "$mode" = window ] || [ "$mode" = verify ]; then
    log_event "$PRIORITY_ERR" "$mode-error" host "$1"
  fi
  printf '%s: %s\n' "$LOG_TAG" "$1" >&2
  exit 1
}

# run_bounded keeps one hung systemctl, docker, or ss call from holding the
# window or the boot check past its budget.
run_bounded() {
  timeout --kill-after="$COMMAND_KILL_GRACE_SECONDS" "$COMMAND_TIMEOUT_SECONDS" "$@"
}

now_seconds() {
  date +%s
}

now_utc() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

report_gap() {
  gaps=$((gaps + 1))
  if [ "$mode" = window ]; then
    log_event "$PRIORITY_WARNING" gap "$1 $2" "$3"
  else
    printf '%s%s %s: %s\n' "$GAP_PREFIX" "$1" "$2" "$3"
  fi
}

first_fields() {
  awk 'NF { print $1 }'
}

# listed tells whether a word is in a space-separated list.
listed() {
  case " $2 " in
    *" $1 "*) return 0 ;;
  esac
  return 1
}

# show_records turns systemctl show blocks into one FS_CHAR-separated line
# per unit: id, unit file state, TriggeredBy, WantedBy and RequiredBy,
# BusName, Names (the id and every alias), then Transient. systemctl prints
# properties in its own order, so the keys are matched by name.
show_records() {
  awk -v sep="$FS_CHAR" '
    function flush() {
      if (id != "") print id sep state sep triggers sep wanted sep bus sep names sep transient
      id = ""; state = ""; triggers = ""; wanted = ""; bus = ""; names = ""; transient = ""
    }
    /^$/ { flush(); next }
    {
      key = $0; sub(/=.*/, "", key)
      value = substr($0, length(key) + 2)
    }
    key == "Id" { id = value }
    key == "UnitFileState" { state = value }
    key == "TriggeredBy" { triggers = value }
    key == "WantedBy" || key == "RequiredBy" { wanted = wanted " " value }
    key == "BusName" { bus = value }
    key == "Names" { names = value }
    key == "Transient" { transient = value }
    END { flush() }'
}

collect_services() {
  listing=$(run_bounded systemctl list-units --type=service --state=running --no-legend --plain --no-pager) ||
    fail_closed "cannot list running services"
  running_services=$(printf '%s\n' "$listing" | first_fields)
  listing=$(run_bounded systemctl list-units --type=target --state=active --no-legend --plain --no-pager) ||
    fail_closed "cannot list active targets"
  active_targets=$(printf '%s\n' "$listing" | first_fields)
  service_records=
  trigger_records=
  [ -n "$running_services" ] || return 0
  # One name per line: a unit name can hold a backslash escape, which
  # xargs would strip without -d.
  shown=$(printf '%s\n' "$running_services" | run_bounded xargs -r -d '\n' systemctl show --no-pager -p Id -p UnitFileState -p TriggeredBy -p WantedBy -p RequiredBy -p BusName -p Names -p Transient) ||
    fail_closed "cannot read the unit files of running services"
  service_records=$(printf '%s\n' "$shown" | show_records)
  triggers=$(printf '%s\n' "$service_records" | awk -F "$FS_CHAR" '{ n = split($3, t, " "); for (i = 1; i <= n; i++) print t[i] }' | sort -u)
  [ -n "$triggers" ] || return 0
  shown=$(printf '%s\n' "$triggers" | run_bounded xargs -r -d '\n' systemctl show --no-pager -p Id -p UnitFileState) ||
    fail_closed "cannot read the unit files of service triggers"
  trigger_records=$(printf '%s\n' "$shown" | show_records)
}

trigger_state() {
  printf '%s\n' "$trigger_records" | awk -F "$FS_CHAR" -v id="$1" '$1 == id { print $2; exit }'
}

target_active() {
  printf '%s\n' "$active_targets" | grep -Fqx -- "$1"
}

service_running() {
  printf '%s\n' "$running_services" | grep -Fqx -- "$1"
}

# generator_wants finds the wants links that systemd generators write under
# /run at every boot, such as a serial console getty; a link that
# systemctl enable --runtime wrote there is gone after a reboot.
generator_wants() {
  for link in /run/systemd/generator*/*.wants/"$1" /run/systemd/generator*/*.requires/"$1"; do
    if [ -e "$link" ] || [ -L "$link" ]; then
      return 0
    fi
  done
  return 1
}

# bus_activated tells whether the system bus starts this unit when a client
# asks for D-Bus name $1. The name's activation file must hand the request to
# systemd as one of the unit's names in $2 (its id or an alias, such as
# dbus-org.freedesktop.timedate1.service). A BusName with no activation file
# only says what the unit claims once it runs, and a file that starts
# another unit, or none, leaves this one down after a reboot. dbus-daemon
# keeps the first file for a name in directory order, so that file decides.
bus_activated() {
  case "$1" in
    '' | */* | .*) return 1 ;;
  esac
  for dir in $DBUS_SYSTEM_SERVICE_DIRS; do
    activation_file="$dir/$1.service"
    grep -Fqsx -- "Name=$1" "$activation_file" || continue
    # More than one key, or an unreadable file, names no unit.
    activated_unit=$(awk -F= -v key="$DBUS_SYSTEMD_SERVICE_KEY" '
      $1 == key { count++; value = substr($0, length(key) + 2) }
      END { if (count == 1) print value }' "$activation_file" 2>/dev/null) || return 1
    case "$activated_unit" in
      '' | *[[:space:]]*) return 1 ;;
    esac
    listed "$activated_unit" "$2"
    return
  done
  return 1
}

# service_comes_back decides whether boot, or the demand that started it,
# starts a running service again: its own enabled unit file, an enabled or
# static socket, timer, or path that triggers it, a target that starts at
# boot, a device, or a running service that wants it, or a D-Bus activation
# file that starts it for its bus name. Neither static (no [Install] section)
# nor disabled (an [Install] section nobody enabled) says how a unit started:
# a D-Bus activated one, such as upower, comes back on demand; one started by
# hand does not. The bus rule skips a masked unit, which no client can start,
# and a transient one, whose definition under /run is gone after a reboot.
service_comes_back() {
  case "$2" in
    enabled | generated) return 0 ;;
    *-runtime)
      generator_wants "$1"
      return
      ;;
    masked | transient) ;;
    *)
      # Transient=yes covers a systemd that reports another unit file state
      # for a transient unit.
      if [ "$7" != yes ] && bus_activated "$5" "$6"; then
        return 0
      fi
      ;;
  esac
  for trigger in $3; do
    case "$(trigger_state "$trigger")" in
      enabled | static | generated | alias) return 0 ;;
    esac
  done
  for wanting in $4; do
    case "$wanting" in
      *.target) target_active "$wanting" && return 0 ;;
      *.device) return 0 ;;
      *.service) service_running "$wanting" && return 0 ;;
    esac
  done
  generator_wants "$1"
}

# skipped_service leaves out user managers, which the linger check covers,
# and the gate's own units.
skipped_service() {
  case "$1" in
    user@*.service | user-runtime-dir@*.service | "$LOG_TAG"*.service) return 0 ;;
  esac
  return 1
}

check_services() {
  while IFS="$FS_CHAR" read -r id state triggers wanted bus names transient; do
    [ -n "$id" ] || continue
    skipped_service "$id" && continue
    service_comes_back "$id" "$state" "$triggers" "$wanted" "$bus" "$names" "$transient" && continue
    report_gap service "$id" "runs, but its unit file state is ${state:-unknown} and nothing that starts at boot wants it: sudo systemctl enable $id, or stop it"
  done <<EOF
$service_records
EOF
}

# unit_state sets unit_answer to a systemctl is-active or is-enabled answer.
# Both exit non-zero for a valid "no", so only an empty answer (a timeout or
# no systemd) means the gate cannot read the unit. It sets a variable rather
# than print, because fail_closed in a command substitution would end only
# the subshell.
unit_state() {
  unit_answer=$(run_bounded systemctl "$1" "$2" 2>/dev/null)
  [ -n "$unit_answer" ] || fail_closed "cannot read $2"
}

# docker_scopes counts container scopes that still hold a process.
docker_scopes() {
  count=0
  for procs in $DOCKER_SCOPE_PROCS_GLOBS; do
    [ -n "$(head -n 1 "$procs" 2>/dev/null)" ] && count=$((count + 1))
  done
  printf '%s\n' "$count"
}

collect_containers() {
  container_records=
  docker_state=missing
  if command -v docker >/dev/null 2>&1; then
    unit_state is-active docker.service
    docker_state=$unit_answer
  fi
  # docker ps answers only while dockerd runs; containers that outlive it
  # are counted from their scopes, so they cannot read as no containers.
  if [ "$docker_state" != active ]; then
    unreadable_containers=$(docker_scopes)
    return 0
  fi
  unreadable_containers=0
  ids=$(run_bounded docker ps -q --no-trunc) || fail_closed "docker does not answer while docker.service is active"
  [ -n "$ids" ] || return 0
  container_records=$(printf '%s\n' "$ids" | run_bounded xargs -r -d '\n' docker inspect --format "$CONTAINER_FORMAT") ||
    fail_closed "cannot inspect running containers"
}

# check_containers accepts only always and unless-stopped. Docker documents
# that on-failure does not restart a container when the daemon restarts; in
# practice it comes back only when the shutdown made it exit with an error,
# so a container that stops cleanly stays down.
check_containers() {
  if [ "$unreadable_containers" -gt 0 ]; then
    report_gap service docker.service "$unreadable_containers containers run while docker.service is $docker_state, so the gate cannot read their restart policies: sudo systemctl enable --now docker.service, then rerun the preflight"
  fi
  [ -n "$container_records" ] || return 0
  unit_state is-enabled docker.service
  docker_enabled=$unit_answer
  if [ "$docker_enabled" != enabled ]; then
    report_gap service docker.service "containers run, but docker.service is ${docker_enabled:-unknown}, so the daemon and its containers do not start after a reboot: sudo systemctl enable docker.service"
  fi
  while IFS="$FS_CHAR" read -r name policy _rest; do
    [ -n "$name" ] || continue
    name=${name#/}
    case "$policy" in
      always | unless-stopped) continue ;;
      on-failure) why="starts it after a reboot only when it exited with an error at shutdown" ;;
      *) why="does not start it after a reboot" ;;
    esac
    report_gap container "$name" "restart policy is ${policy:-no}, which $why: docker update --restart unless-stopped $name, or restart: unless-stopped in its compose file"
  done <<EOF
$container_records
EOF
}

# collect_listeners prints protocol, local address, pid, and process name of
# every listening TCP and UDP socket, one line per owning process.
collect_listeners() {
  sockets=$(run_bounded ss -Hlntup) || fail_closed "cannot list listening sockets"
  listener_records=$(printf '%s\n' "$sockets" | awk -v sep="$FS_CHAR" '
    {
      rest = $0
      while (match(rest, /\("[^"]*",pid=[0-9]+/)) {
        item = substr(rest, RSTART + 2, RLENGTH - 2)
        rest = substr(rest, RSTART + RLENGTH)
        name = item; sub(/".*/, "", name)
        pid = item; sub(/.*pid=/, "", pid)
        print $1 sep $5 sep pid sep name
      }
    }')
}

# check_listeners flags a listener whose process lives in a scope: login
# sessions (a shell, tmux, screen, or nohup), transient scopes in a user
# manager, and systemd-run scopes. systemd never starts a scope again, so
# the process is gone after a reboot. Container scopes are left to the
# restart-policy check.
check_listeners() {
  while IFS="$FS_CHAR" read -r proto local pid name; do
    [ -n "$pid" ] || continue
    cgroup=$(sed -n 's/^0:://p' "/proc/$pid/cgroup" 2>/dev/null) || continue
    case "$cgroup" in
      */session-*.scope) where="a login session" ;;
      */user@*.service/init.scope) continue ;;
      */user@*.service/*.scope) where="a transient scope of a user manager" ;;
      */run-*.scope) where="a systemd-run scope" ;;
      *) continue ;;
    esac
    report_gap listener "$proto $local" "process $name (pid $pid) runs in $where ($cgroup), which does not start after a reboot: run it as an enabled systemd service or as a container with restart policy unless-stopped"
  done <<EOF
$listener_records
EOF
}

# check_user_managers flags services in the systemd manager of a user
# without linger: that manager stops at the user's last logout and does not
# start at boot. Socket-activated user services start on demand, so only
# services nothing triggers count.
check_user_managers() {
  for unit in $running_services; do
    case "$unit" in
      user@*.service) ;;
      *) continue ;;
    esac
    uid=${unit#user@}
    uid=${uid%.service}
    case "$uid" in
      '' | *[!0-9]*) continue ;;
    esac
    entry=$(run_bounded getent passwd "$uid") || entry=
    account=${entry%%:*}
    if [ -z "$account" ]; then
      report_gap user "uid $uid" "a systemd user manager runs for a uid the account database does not resolve, so the gate cannot check its services: check getent passwd $uid"
      continue
    fi
    [ -e "$LINGER_DIR/$account" ] && continue
    if ! listing=$(run_bounded systemctl --user --machine="$account@" list-units --type=service --state=running --no-legend --plain --no-pager 2>/dev/null); then
      report_gap user "$account" "cannot read the services of this user's systemd manager"
      continue
    fi
    user_services=$(printf '%s\n' "$listing" | first_fields)
    [ -n "$user_services" ] || continue
    if ! shown=$(printf '%s\n' "$user_services" | run_bounded xargs -r -d '\n' systemctl --user --machine="$account@" show --no-pager -p Id -p TriggeredBy 2>/dev/null); then
      report_gap user "$account" "cannot read the services of this user's systemd manager"
      continue
    fi
    untriggered=$(printf '%s\n' "$shown" | show_records | awk -F "$FS_CHAR" '$3 == "" { print $1 }')
    for service in $untriggered; do
      report_gap user-service "$account/$service" "runs in a user manager that stops at logout and does not start after a reboot: sudo loginctl enable-linger $account, then systemctl --user enable $service as $account"
    done
  done
}

collect() {
  collect_services
  collect_containers
  collect_listeners
}

find_gaps() {
  check_services
  check_containers
  check_listeners
  check_user_managers
}

ephemeral_port_floor() {
  floor=$(awk '{ print $1; exit }' "$EPHEMERAL_PORT_RANGE_PATH" 2>/dev/null)
  case "$floor" in
    '' | *[!0-9]*) floor=$DEFAULT_EPHEMERAL_PORT_FLOOR ;;
  esac
  printf '%s\n' "$floor"
}

# tcp_listeners prints the local address of each TCP listener on a port the
# operator chose, so the boot check compares stable ports only.
tcp_listeners() {
  sockets=$(run_bounded ss -Hlntu) || return 1
  printf '%s\n' "$sockets" | awk -v floor="$(ephemeral_port_floor)" '
    $1 == "tcp" {
      port = $5; sub(/.*:/, "", port)
      if (port + 0 < floor + 0) print $5
    }' | sort -u
}

# snapshot_units keeps the running services that boot starts again: enabled
# or generated units that an active target wants. A service started on
# demand, such as a D-Bus activated one, is not expected back after boot.
snapshot_units() {
  while IFS="$FS_CHAR" read -r id state _triggers wanted _bus _names _transient; do
    [ -n "$id" ] || continue
    skipped_service "$id" && continue
    case "$state" in
      enabled | generated) ;;
      *) continue ;;
    esac
    for wanting in $wanted; do
      case "$wanting" in
        *.target)
          if target_active "$wanting"; then
            printf 'unit=%s\n' "$id"
            break
          fi
          ;;
      esac
    done
  done <<EOF
$service_records
EOF
}

snapshot_body() {
  printf 'time=%s\nepoch=%s\nboot=%s\n' "$(now_utc)" "$(now_seconds)" "$boot_id"
  snapshot_units
  while IFS="$FS_CHAR" read -r name _policy image health; do
    [ -n "$name" ] || continue
    printf 'container=%s image=%s health=%s\n' "${name#/}" "$image" "$health"
  done <<EOF
$container_records
EOF
  listeners=$(tcp_listeners) || return 1
  if [ -n "$listeners" ]; then
    printf '%s\n' "$listeners" | sed 's/^/listen=/'
  fi
}

ensure_state_dir() {
  [ -d "$STATE_DIR" ] || install -d -m 0700 "$STATE_DIR" || fail_closed "cannot create $STATE_DIR"
}

# write_private writes stdin to a root-only file through a rename, so a
# reader never sees half a snapshot or result.
write_private() {
  ensure_state_dir
  tmp=$(mktemp "$STATE_DIR/.$LOG_TAG.XXXXXX") || return 1
  if cat >"$tmp" && chmod 0600 "$tmp" && mv -f "$tmp" "$1"; then
    return 0
  fi
  rm -f "$tmp"
  return 1
}

pending_packages() {
  if [ -r "$REBOOT_REQUIRED_PATH.pkgs" ]; then
    sort -u "$REBOOT_REQUIRED_PATH.pkgs" | tr '\n' ' '
  else
    printf 'pending updates'
  fi
}

window() {
  if [ ! -e "$REBOOT_REQUIRED_PATH" ]; then
    log_event "$PRIORITY_INFO" window-skip host "no reboot is pending"
    return 0
  fi
  collect
  find_gaps
  if [ "$gaps" -gt 0 ]; then
    log_event "$PRIORITY_WARNING" reboot-skipped host "$gaps running workloads would not come back, so the pending reboot waits; sudo $LOG_TAG preflight lists them"
    return 0
  fi
  boot_id=$(cat "$BOOT_ID_PATH") || fail_closed "cannot read the boot ID"
  snapshot=$(snapshot_body) || fail_closed "cannot read listeners for the snapshot"
  printf '%s\n' "$snapshot" | write_private "$SNAPSHOT_PATH" || fail_closed "cannot write $SNAPSHOT_PATH"
  log_event "$PRIORITY_NOTICE" rebooting host "preflight passed; rebooting for $(pending_packages)"
  if ! run_bounded systemctl reboot; then
    # A snapshot left behind would drive the check after some later,
    # unrelated reboot.
    rm -f "$SNAPSHOT_PATH"
    fail_closed "systemctl reboot failed"
  fi
}

container_state() {
  run_bounded docker inspect --format "$CONTAINER_STATE_FORMAT" "$1" 2>/dev/null
}

# retire_snapshot keeps the snapshot for review but stops it from driving a
# check, so a later boot that the gate did not start is not compared with
# it.
retire_snapshot() {
  mv -f "$SNAPSHOT_PATH" "$SNAPSHOT_PATH.last"
}

# write_result records a status and its findings for doctor, tied to the
# boot that was checked.
write_result() {
  {
    printf 'time=%s\nboot=%s\nstatus=%s\n' "$(now_utc)" "$boot_id" "$1"
    if [ -n "$2" ]; then
      printf '%s\n' "$2" | sed 's/^/finding=/'
    fi
  } | write_private "$RESULT_PATH"
}

# stale_snapshot prints why a snapshot cannot describe this boot. A
# snapshot from the running boot means the gated reboot never happened; an
# old one means this boot followed a later manual, provider, or crash
# reboot. A check against either could start a workload that the operator
# stopped since.
stale_snapshot() {
  snapshot_boot=$(sed -n 's/^boot=//p' "$SNAPSHOT_PATH")
  written=$(sed -n 's/^epoch=//p' "$SNAPSHOT_PATH")
  if [ "$snapshot_boot" = "$boot_id" ]; then
    printf 'the snapshot is from the running boot, so the gated reboot did not happen'
    return 0
  fi
  case "$written" in
    '' | *[!0-9]*)
      printf 'the snapshot has no write time'
      return 0
      ;;
  esac
  age=$(($(now_seconds) - written))
  if [ "$age" -lt 0 ] || [ "$age" -gt "$SNAPSHOT_MAX_AGE_SECONDS" ]; then
    printf 'the snapshot was written %s seconds before this boot check, outside 0 to %s, so this boot did not follow the gated reboot' "$age" "$SNAPSHOT_MAX_AGE_SECONDS"
    return 0
  fi
  return 1
}

# verify_stopped records a check that systemd stopped at its runtime limit,
# or that an operator stopped, so the boot never reads as checked.
verify_stopped() {
  trap - TERM INT
  write_result degraded "the post-reboot check was stopped before it finished"
  retire_snapshot
  log_event "$PRIORITY_ERR" verify-error host "the check was stopped before it finished; $RESULT_PATH records it"
  exit 1
}

# find_missing compares the host with the snapshot. A container whose health
# check still starts is not missing yet, so the wait goes on for it.
find_missing() {
  missing_units=
  missing_containers=
  starting_containers=
  missing_listeners=
  for unit in $expected_units; do
    run_bounded systemctl is-active --quiet "$unit" || missing_units="$missing_units $unit"
  done
  for name in $expected_containers; do
    state=$(container_state "$name")
    case "$state" in
      'true starting') starting_containers="$starting_containers $name" ;;
      true\ *) ;;
      *) missing_containers="$missing_containers $name" ;;
    esac
  done
  [ -n "$expected_listeners" ] || return 0
  current=$(tcp_listeners) || current=
  missing_listeners=$(printf '%s\n' "$expected_listeners" | while IFS= read -r address; do
    printf '%s\n' "$current" | grep -Fqx -- "$address" || printf '%s\n' "$address"
  done)
}

wait_for_workloads() {
  while :; do
    find_missing
    [ -n "$missing_units$missing_containers$starting_containers$missing_listeners" ] || return 0
    [ "$(now_seconds)" -lt "$1" ] || return 0
    sleep "$VERIFY_POLL_SECONDS"
  done
}

# start_missing_once gives each missing unit and container from the
# snapshot one start, and nothing else: a workload that was not running
# before the reboot is never started.
start_missing_once() {
  attempted=0
  restarted_units=
  restarted_containers=
  # An attempt starts only when its worst case ends before the deadline, so
  # the restarts never run into the unit's runtime limit.
  attempt_seconds=$((COMMAND_TIMEOUT_SECONDS + RESTART_TIMEOUT_SECONDS + 2 * COMMAND_KILL_GRACE_SECONDS))
  for unit in $missing_units; do
    [ "$(($(now_seconds) + attempt_seconds))" -le "$1" ] || break
    attempted=1
    restarted_units="$restarted_units $unit"
    log_event "$PRIORITY_WARNING" restart "$unit" "not active after the wait; restarting it once"
    run_bounded systemctl reset-failed "$unit" 2>/dev/null
    if timeout --kill-after="$COMMAND_KILL_GRACE_SECONDS" "$RESTART_TIMEOUT_SECONDS" systemctl restart "$unit"; then
      log_event "$PRIORITY_INFO" restart-result "$unit" "restart returned success"
    else
      log_event "$PRIORITY_ERR" restart-result "$unit" "restart failed"
    fi
  done
  for name in $missing_containers; do
    [ "$(($(now_seconds) + attempt_seconds))" -le "$1" ] || break
    attempted=1
    restarted_containers="$restarted_containers $name"
    log_event "$PRIORITY_WARNING" restart "$name" "container not running after the wait; starting it once"
    if timeout --kill-after="$COMMAND_KILL_GRACE_SECONDS" "$RESTART_TIMEOUT_SECONDS" docker start "$name" >/dev/null; then
      log_event "$PRIORITY_INFO" restart-result "$name" "docker start returned success"
    else
      log_event "$PRIORITY_ERR" restart-result "$name" "docker start failed"
    fi
  done
}

# verify_findings lists what is still wrong: missing workloads and ports
# from the snapshot, any failed unit, and a container that was healthy
# before the reboot but is not now.
verify_findings() {
  for unit in $missing_units; do
    if listed "$unit" "$restarted_units"; then
      printf 'unit %s is not active after the wait and one restart\n' "$unit"
    else
      printf 'unit %s is not active after the wait; the restart budget ended before its restart\n' "$unit"
    fi
  done
  for name in $missing_containers; do
    if listed "$name" "$restarted_containers"; then
      printf 'container %s is not running after the wait and one start\n' "$name"
    else
      printf 'container %s is not running after the wait; the restart budget ended before its start\n' "$name"
    fi
  done
  for name in $starting_containers; do
    printf 'container %s health check is still starting\n' "$name"
  done
  if [ -n "$missing_listeners" ]; then
    printf '%s\n' "$missing_listeners" | sed 's/^/nothing listens on tcp /; s/$/, which listened before the reboot/'
  fi
  if listing=$(run_bounded systemctl list-units --state=failed --no-legend --plain --no-pager); then
    for unit in $(printf '%s\n' "$listing" | first_fields); do
      # A missing unit from the snapshot is already reported above.
      listed "$unit" "$missing_units" && continue
      printf 'unit %s failed\n' "$unit"
    done
  else
    printf 'cannot list failed units\n'
  fi
  sed -n 's/^container=\([^ ]*\) image=.* health=healthy$/\1/p' "$SNAPSHOT_PATH" | while IFS= read -r name; do
    state=$(container_state "$name")
    case "$state" in
      'true healthy' | '') ;;
      *) printf 'container %s was healthy before the reboot and is now %s\n' "$name" "${state#true }" ;;
    esac
  done
}

verify() {
  if [ ! -f "$SNAPSHOT_PATH" ]; then
    log_event "$PRIORITY_INFO" verify-skip host "no snapshot from a gated reboot"
    return 0
  fi
  boot_id=$(cat "$BOOT_ID_PATH") || fail_closed "cannot read the boot ID"
  if reason=$(stale_snapshot); then
    write_result stale "$reason" || fail_closed "cannot write $RESULT_PATH"
    retire_snapshot || fail_closed "cannot retire $SNAPSHOT_PATH"
    log_event "$PRIORITY_ERR" verify-stale host "$reason; no workload was compared or started"
    exit 1
  fi
  trap verify_stopped TERM INT
  expected_units=$(sed -n 's/^unit=//p' "$SNAPSHOT_PATH")
  expected_containers=$(sed -n 's/^container=\([^ ]*\) .*/\1/p' "$SNAPSHOT_PATH")
  expected_listeners=$(sed -n 's/^listen=//p' "$SNAPSHOT_PATH")
  deadline=$(($(now_seconds) + VERIFY_TIMEOUT_SECONDS))
  restart_deadline=$((deadline + RESTART_BUDGET_SECONDS))
  wait_for_workloads "$deadline"
  start_missing_once "$restart_deadline"
  if [ "$attempted" = 1 ]; then
    # A restart returns once its start job ends, so only listeners and
    # health checks still need time; a unit that failed again does not.
    wait_for_workloads "$(($(now_seconds) + RESTART_SETTLE_SECONDS))"
  fi
  findings=$(verify_findings)
  status=ok
  [ -z "$findings" ] || status=degraded
  # From here a stop leaves the snapshot in place, which doctor reports, and
  # never overwrites the finished result.
  trap - TERM INT
  write_result "$status" "$findings" || fail_closed "cannot write $RESULT_PATH"
  retire_snapshot || fail_closed "cannot retire $SNAPSHOT_PATH"
  if [ "$status" = ok ]; then
    log_event "$PRIORITY_INFO" verify-ok host "every workload from the snapshot came back"
    return 0
  fi
  printf '%s\n' "$findings" | while IFS= read -r finding; do
    log_event "$PRIORITY_WARNING" finding host "$finding"
  done
  log_event "$PRIORITY_ERR" verify-degraded host "workloads did not all come back; $RESULT_PATH lists them"
  exit 1
}

case "$mode" in
  preflight)
    collect
    find_gaps
    [ "$gaps" -eq 0 ] || exit "$GAP_EXIT_CODE"
    ;;
  window) window ;;
  verify) verify ;;
  *)
    printf 'usage: %s preflight|window|verify\n' "$0" >&2
    exit "$USAGE_EXIT_CODE"
    ;;
esac
