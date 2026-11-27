# Desktop and phone apps

<!-- guide-sources:
desktop/first_run.go
desktop/device_owner.go
desktop/background_desktop.go
desktop/tray_desktop.go
desktop/notices_fetch.go
desktop/notices_ios.m
desktop/startup_recovery.go
web/src/locales/en/firstInstall.json
web/src/locales/en/desktopBackground.json
web/src/locales/en/authGate.json
web/src/locales/en/workspaceSettingsDialog.json
-->

This chapter covers Leafen's apps: the desktop app for macOS and Windows, and
the phone apps. It shows how an app starts, how a phone or another computer
joins it, how to log the app in to another Leafen, and how notifications
reach each device. Downloading and installing the desktop app is in
[Installing Leafen](../install.md#desktop-app); updates, background serving and the
app's own server are in
[Self-hosting: Upgrades](../self-hosting.md#upgrades).

## The desktop app is a server

The desktop app runs a whole Leafen server on your computer, with your data
in the app's data folder. Your phone, your other computers and the people you
invite join that server. It stays reachable only while the computer is on
and awake; for a server that is always on, move to Docker later (see
[Backups](backups.md) and
[Self-hosting: Moving to another server](../self-hosting.md#moving-to-another-server)).

The desktop app needs macOS 13 Ventura or later on a Mac. There is no Linux
desktop app; on Linux, Leafen runs in Docker and you use it in a browser.

## The first start

The app asks **How would you like to start?**

- **Start new on this computer**: this computer becomes the server. Enter your
  name and choose a language; the window is the account's first device and
  stays logged in. Then the app asks how other devices reach it (see
  [Outside address](outside-address.md)) and how agents connect (see
  [Agents](agents.md)); each step has **Later**.
- **Log in**: for a Leafen you already use, on a company or team server or
  another device. Paste the **Invite link or Leafen address**, or **Scan a QR
  code**, then log in to that Leafen with its own login screen. The
  computer then runs no server of its own.

What you skipped comes back under Settings → **Preferences** → **Install** →
**Finish what was skipped at install**.

On a phone, the first choice is **Try it on this phone** instead: Leafen on
the phone for one person to try. It stops when the app closes, opens no
outside address and runs no apps; move it to a computer or server later.

## Add a phone or another computer

1. In the app, open the workspace name in the sidebar → **Add a device**. It
   shows a QR code and a code.
2. On the phone, scan the QR code with the camera or the Leafen phone app, or
   choose **Log in with a code** on the other device's login screen and type
   the code.

The QR code holds the server's outside address. Until the server has one, only
this computer can open it, and **Add a device** says so; turn one on first, as
[Outside address](outside-address.md) shows. The code works once, for 10
minutes. [Accounts and devices](accounts-and-devices.md) has every way to
log in.

To open the desktop app's workspace in a browser on the same computer, use
Settings → your name → **Open in a browser**. That browser logs in as a new
device of your account.

## Log in as this device's owner

On the login page of the app's own server, the desktop app and the iPhone
app first offer **Log in as this device's owner**: Touch ID, Face ID or the
Mac's password on Apple devices, and Windows Hello or the Windows password on
Windows. The app then logs that device in itself. This works only in the
app, for the server running inside it; a browser can't ask for it.

## More than one server

One app can be logged in to several Leafen servers: its own and, for example,
a company server. Settings → **Server accounts** lists them; **Add account**
logs in to another server or another account, and you work on one server at
a time. The workspace menu's **Add account** adds another account on the same
server.

## When the app's computer serves others

While other devices use the desktop app's server (an outside address is on),
closing the window leaves Leafen running in the menu bar on macOS or the tray
on Windows, with **Quit Leafen** in its menu. Settings → **Server accounts** →
**This computer** holds the switches:

- **Keep in background**: hide Leafen instead of quitting when its last window
  closes.
- **Open at login**: while the computer serves, Leafen opens when you log in.
  macOS may ask you to allow it in System Settings → General → Login Items.
- **Keep this computer awake while it serves**: the computer doesn't sleep,
  though the display turns off. **Also on battery** is off by default.
- **Use Command Search** and **Command Search shortcut**: a shortcut opens
  Leafen's search even when the app is closed.

## Notifications

| Where | How notifications arrive |
| --- | --- |
| A desktop browser | Web Push, once you turn on **Desktop app push notifications** in Settings → **Notifications** and allow it in the browser. |
| The desktop app | The system's notifications while the app runs; clicking one opens its page. |
| Android, in the browser | Web Push, as on a desktop browser. |
| iPhone or iPad, in Safari | Only after **Share → Add to Home Screen** (iOS 16.4 or later): open Leafen from the Home Screen and turn notifications on there. |
| The phone apps | No push. While open, the app keeps the unread count on its icon. When the phone lets it check in the background, at most every 15 minutes, it asks the server what came since and notifies you. |

Push notifications are for mentions, replies and pages you follow that you
have not seen after 10 seconds. Leafen runs no push service of its own.

## When the app's server can't start

If the app's own server can't start, because another program holds its
database, the database is damaged or the disk is full, or it takes longer than
15 seconds, the window says why. It offers to try again, copy the details,
save the log and open the data folder, and, where the database won't open, to
restore the last scheduled backup (see [Backups](backups.md)).

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Device code from **Add a device** | Once, 10 minutes | Renewed while the window is open. |
| Phone background check | At most every 15 minutes | As the phone allows. |
| Push after unseen | 10 seconds | Mentions, replies and pages you follow. |
| **Also on battery** | Off | Keeping awake works on a power adapter only, unless turned on. |
| macOS version | 13 Ventura or later | |

## For agents and the API

An agent installing the desktop app for someone follows
[Installing Leafen: For agents](../install.md#for-agents): `leafen setup` takes the
person through the first run from the terminal, and
`leafen setup sign-in <invite link or address> --code <code>` logs the app in
to a Leafen they already use. A device code for the app's own server comes from
`leafen account device-code`, run as the app's program.

## Differences from Notion

- Notion's apps open Notion's servers. Leafen's desktop app is itself the
  server, and other devices join it.
- Notion's phone apps get push from Notion's servers. Leafen runs no push
  service: browsers get Web Push, and the phone apps check in the background
  instead.

## Related chapters

- [Accounts and devices](accounts-and-devices.md)
- [Outside address](outside-address.md)
- [Backups](backups.md)
- [Installing Leafen](../install.md)
