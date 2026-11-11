# Included in the generated portable installer. All old application files are
# backed up before replacing them; only compatibility links use the old name.
migrate_legacy_install() {
    legacy_app="$HOME/.local/screenpin.app"
    legacy_binary="$HOME/.local/bin/screenpin"
    backup_root="$data_home/framely/install-backups"
    backup=
    # The former local developer installation used a regular binary plus this
    # registered desktop entry. Do not claim an unrelated command with its name.
    if [ -f "$legacy_binary" ] && [ ! -L "$legacy_binary" ] &&
       [ -d "$data_home/screenpin/workers" ] &&
       [ -f "$data_home/applications/io.screenpin.desktop.desktop" ] &&
       grep -qF "$HOME/.local/bin/" "$data_home/applications/io.screenpin.desktop.desktop"; then
        mkdir -p "$backup_root"
        backup=$(mktemp -d "$backup_root/upgrade-XXXXXX")
        mv "$legacy_binary" "$backup/screenpin"
        mv "$data_home/screenpin/workers" "$backup/workers"
        rmdir "$data_home/screenpin" 2>/dev/null || true
    fi
    if [ -d "$legacy_app" ] && [ ! -L "$legacy_app/screenpin" ] && [ -f "$legacy_app/screenpin" ]; then
        if [ -z "$backup" ]; then
            mkdir -p "$backup_root"
            backup=$(mktemp -d "$backup_root/upgrade-XXXXXX")
        fi
        mv "$legacy_app" "$backup/legacy-app"
        # Preserve old absolute shortcuts without leaving an old executable.
        mkdir -p "$legacy_app"
        ln -s ../framely.app/framely "$legacy_app/screenpin"
    fi
    if [ -n "$backup" ]; then echo "Previous installation backed up to $backup"; fi
    for old_desktop in "$data_home/applications/screenpin.desktop" "$data_home/applications/framely.desktop"; do
        if [ -f "$old_desktop" ] &&
           grep -qF -e "$HOME/.local/screenpin.app/" -e "$HOME/.local/bin/screenpin" "$old_desktop"; then
            if [ -z "$backup" ]; then
                mkdir -p "$backup_root"
                backup=$(mktemp -d "$backup_root/upgrade-XXXXXX")
            fi
            mv "$old_desktop" "$backup/$(basename "$old_desktop")"
        fi
    done
    autostart="${XDG_CONFIG_HOME:-$HOME/.config}/autostart/io.screenpin.desktop.desktop"
    if [ -f "$autostart" ]; then
        # Keep Hidden and X-GNOME-Autostart-enabled as the user set them.
        awk -v executable="$app_dir/framely" '
            /^Exec=/ { print "Exec=\"" executable "\" --hidden"; next }
            /^Name=/ { print "Name=Framely"; next }
            { print }
        ' "$autostart" > "$autostart.framely-upgrade"
        mv "$autostart.framely-upgrade" "$autostart"
    fi
}

legacy_command_link() {
    [ -L "$1" ] || return 1
    case "$(readlink "$1")" in
        "$HOME/.local/screenpin.app/screenpin"|"$HOME/.local/bin/screenpin"|screenpin|framely) return 0 ;;
        *) return 1 ;;
    esac
}

remove_legacy_install_link() {
    legacy="$HOME/.local/screenpin.app/screenpin"
    if [ -L "$legacy" ] && [ "$(readlink "$legacy")" = ../framely.app/framely ]; then
        rm "$legacy"
        rmdir "$HOME/.local/screenpin.app" 2>/dev/null || true
    fi
}
