# Web UI development

The dashboard is React, TypeScript, Vite, and Tailwind, and it lives in
`webui/`. Run every command below from the repository root. For the toolchain
versions, see [Build](../README.md#build).

## Run the dashboard against the Go server

```bash
npm ci --prefix webui
make build
./bin/dnsbench -ui -listen 127.0.0.1:8080
```

Then open <http://127.0.0.1:8080>.

`make build` runs `npm install` for you when `webui/node_modules` is missing.
Run `npm ci` yourself when you want exactly what `package-lock.json` records,
which is what CI installs.

The binary embeds the built assets, so a frontend edit does nothing until you
stop the server and run `make build` again.

## Preview layout changes with Vite

```bash
npm run dev --prefix webui -- --host 127.0.0.1
```

Open the URL Vite prints. Hot reload makes this the fast way to iterate on
layout, and it is good for nothing else: the dashboard calls `/api` on its own
origin, `vite.config.ts` configures no proxy, and so the defaults, the benchmark
controls, and the live events all fail until you go back to the Go server.
`make ui-dev` runs the same Vite server but binds it to every interface.

## Check your changes

```bash
npm run lint --prefix webui
npm run build --prefix webui
```

Then start the Go server and walk through the dashboard:

1. Confirm that the default domains and resolvers load.
2. Start a benchmark. Watch the live query log and the results fill in.
3. Stop the run. Confirm that the controls return to their idle state.
4. Click **Reset**. Confirm that the results clear.

Commit `package.json` and `package-lock.json` together, and run
`npm audit --prefix webui` after any dependency change.
