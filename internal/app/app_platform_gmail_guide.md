# Connecting Gmail to Leafen

Leafen's built-in Mail app reads and sends a person's Gmail. Each person connects
their own Gmail once, in Leafen; every agent connected to Leafen then uses it
through the Mail app's tools (`leafen_app_tools`, `leafen_app_call`). Sending
mail waits for the person's approval in Leafen. A password, app password or
client secret is never asked for in this conversation nor written in an answer:
it goes into Leafen's own window — typed there by the agent in its tab when it
made it with a browser tool and Leafen is open there, else by them — and the app
never sees it.

{{doing-outside-steps-with-the-person}}

## First: an app password (no setup on the server)

This is the way offered first. It goes as above — with a browser tool, by the
agent, after one question for a yes carrying that an app password named
“Leafen” gets made (Google asks them for their own password first: that is
theirs):

1. The administrator has installed the Mail app (Settings → Server → Apps → Browse apps,
   `<Leafen's address>/admin/settings?section=apps`) and, in the Mail app's
   "Secrets" window, turned on "Let people give this
   app their own account details". If the Mail app says the administrator
   hasn't allowed it, that is the administrator's to do: a line to them
   carries it.
2. The person turns on 2-Step Verification for their Google account, if it isn't
   on yet: open https://myaccount.google.com/security for them.
3. Make an app password: open https://myaccount.google.com/apppasswords;
   any name, such as "Leafen"; Google shows its 16 letters once.
4. Open Leafen's Mail app (`<Leafen's address>/apps/mail`): Gmail →
   "Connect Gmail", where their Gmail address and the app password go into
   Leafen's window.

Leafen then reads mail over IMAP (imap.gmail.com) and sends over SMTP
(smtp.gmail.com) with them. If Gmail refuses the login, the app password is
wrong or was removed; making a new one and connecting again fixes it.

App passwords aren't available when 2-Step Verification is off, for accounts
with only security keys or Advanced Protection, or when a Google Workspace
administrator turned them off. Only then use a Google sign-in.

## Only if needed: a Google sign-in (a Google client the administrator makes once)

The server's administrator makes a Google OAuth client once; then every person
can choose "Sign in with Google instead" in the Mail app. Go through it with the
administrator as above — with a browser tool, yourself, after one yes to make
the Google project and client below; without one, a step at a time:

1. Open https://console.cloud.google.com for them, signed in to the Google
   account that will own the client, and create a project (or choose one),
   such as "Leafen Mail".
2. APIs & Services → Library: find "Gmail API" and enable it.
3. Google Auth Platform (OAuth consent screen): start it with an app name such as
   "Leafen", a support email and a contact email.
   - Audience: "Internal" when everyone is in the organization's Google
     Workspace — no review needed. Otherwise "External": while the app stays in
     "Testing", add each person's Gmail address as a test user, and know that
     Google ends test logins after 7 days (people log in again); publishing it
     for everyone needs Google's review of the Gmail scope.
   - Data access: add the scope `https://www.googleapis.com/auth/gmail.modify`.
4. Clients → Create client → Application type "Web application". Under
   "Authorized redirect URIs" add Leafen's address for returning logins,
   exactly as the Mail app's "Secrets" window in Leafen shows it — it is
   `<Leafen's address>/api/leafen/v1/apps/oauth/callback`. Create it.
5. Google shows a client ID and a client secret. They go, in Leafen, into
   Settings → Server → Apps → Mail → Secrets
   (`<Leafen's address>/admin/settings?section=apps`):
   `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, typed by you in your tab or
   by the administrator. Don't take them in this conversation.
6. Each person then opens the Mail app → Gmail → "Sign in with Google instead"
   and approves on Google's page.

There is no shared login run by Leafen's makers: each server uses its own
client, so mail never passes through anyone else.
