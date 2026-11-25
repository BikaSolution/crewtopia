# Shift Manager

## Overview

I want to create a shift manager. A website. Problem is, using normal JS won't work since I need a mini sqlite db to store things. I don't know what technology is best. A complete django app is going to far. Maybe a react? Or small go application as BE? Not sure.

## Requirements

We need 5 pages in total. A login page, a shifts setup, a staff pool page, a view only roster page and a editable roster page.

A staff member can look something like this:

Name, Nickname, Contact number, Desire, Shifts taken, Shifts available, Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sonday
Ryk, TestM 1, 21, 38.00%, 7, Barwench, Barman, Manager, TRUE, FALSE, TRUE, FALSE, FALSE, FALSE, FALSE

A shift can look something like this:

Name, Start, End, Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sonday
Barman, 17:00, 21:00, FALSE, TRUE, TRUE, TRUE, FALSE, TRUE, TRUE

You then need to be able to click the generate button on the editable roster page, given a starting date.

All this information needs to be stored in sqlite.

As for how the people are chosen, shifts next to each other is seen as unfavourable, higher desire is more favourable (math.Random() * desire) to increase probability, of course, no two shifts for the same person can overlap, but the can start and end on the same, eg. 16h->17h; 17h->18h.

## Infrastructure

I have a domain, what I don't have is a public facing endpoint. I heard that you can do a tunnel setup with cloudflare. Think that might help.

Would be great to have the whole thing containerized with testing.

Login would be great using ory, can also live in docker container.

## Running it

Crewtopia runs as a Docker Compose stack: Ory Kratos (identity), the Go app,
Ory Oathkeeper (access proxy in front of the app) and `cloudflared`
(Cloudflare Tunnel). No ports are published on the host; public traffic
arrives only through the tunnel.

Hardening, all in `docker-compose.yml`:

- Every container runs non-root with a read-only root filesystem, no Linux
  capabilities, `no-new-privileges`, and memory/CPU/process limits.
- Kratos, the app and Oathkeeper sit on an internal `backend` network with no
  internet access. `cloudflared` shares a separate internal `edge` network with
  Oathkeeper only, so it can't reach the app or Kratos directly. The app
  trusts the `X-User-*` headers Oathkeeper sets, which is only safe while
  that holds, so never add `ports:` or put `cloudflared` on `backend`.
- Oathkeeper's management API listens on the container's loopback only.
- A one-shot `kratos-migrate` service applies Kratos SQL migrations before
  Kratos starts, so Kratos image upgrades don't need a manual step.

### Reaching services locally

Instead of publishing ports, run commands inside the stack:

```sh
# Kratos admin API (identities, invites) — from inside the kratos container
docker compose exec kratos wget -qO- http://127.0.0.1:4434/admin/identities

# The proxy, as the tunnel sees it
docker run --rm --network crewtopia_edge curlimages/curl:8.17.0 -s \
  -H 'Host: crewtopia.bikasolutions.co.za' -H 'X-Forwarded-Proto: https' \
  http://oathkeeper:4455/roster
```

Secrets are never committed. Before the first start:

```sh
cp .env.example .env                                            # Cloudflare Tunnel token
cp deploy/kratos/secrets.example.yml deploy/kratos/secrets.yml  # Kratos cookie/cipher secrets
# generate the values as described in secrets.example.yml
docker compose up -d --build
```

`deploy/kratos/secrets.yml` is merged on top of `kratos.yml` through a second
`-c` flag (see `docker-compose.yml`). Both `.env` and `secrets.yml` are
git-ignored.

The app container runs as a non-root user (uid 65532) with a read-only root
filesystem. A volume created by an older, root-running version needs a
one-time ownership fix:

```sh
docker compose stop app
docker run --rm -v crewtopia_app-data:/data alpine chown -R 65532:65532 /data
docker compose up -d --build app
```

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
