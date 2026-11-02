import type { ConnectionInfo } from './api'

// Connection snippets for every common client. They are generated in the
// browser from ConnectionInfo, so the password never leaves the response it
// was shown in (when absent, a placeholder is used).

export interface Snippet {
  id: string
  title: string
  description: string
  language: string
  code: string
}

const PLACEHOLDER = '<CONTRASEÑA>'

export function buildSnippets(c: ConnectionInfo, password?: string): Snippet[] {
  const pass = password ?? PLACEHOLDER
  const port = c.port
  const uri = `postgresql://${c.username}:${encodeURIComponent(pass)}@localhost:${port}/${c.database}`
  const jdbc = `jdbc:postgresql://localhost:${port}/${c.database}`
  const out: Snippet[] = []

  if (!c.localMode) {
    out.push(
      {
        id: 'cloudflared',
        title: 'Abrir el túnel (prueba rápida)',
        description: 'Ejecútalo en la máquina cliente y déjalo abierto. Expone la base en localhost.',
        language: 'bash',
        code: `cloudflared access tcp --hostname ${c.hostname} --url localhost:${port}`,
      },
      {
        id: 'compose',
        title: 'Docker Compose (sidecar)',
        description: 'El túnel arranca y se reinicia junto a tu app. Tu app se conecta a "postern:5432".',
        language: 'yaml',
        code: `services:
  postern:
    image: cloudflare/cloudflared:latest
    command: access tcp --hostname ${c.hostname} --url 0.0.0.0:5432
    restart: unless-stopped

  app:
    # tu servicio
    environment:
      SPRING_DATASOURCE_URL: jdbc:postgresql://postern:5432/${c.database}
      SPRING_DATASOURCE_USERNAME: ${c.username}
      SPRING_DATASOURCE_PASSWORD: \${DB_PASSWORD}
    depends_on: [postern]`,
      },
      {
        id: 'systemd',
        title: 'Servicio systemd (Linux)',
        description: 'Guárdalo en /etc/systemd/system/postern-' + c.database + '.service y ejecuta: sudo systemctl enable --now postern-' + c.database,
        language: 'ini',
        code: `[Unit]
Description=PosternDB tunnel to ${c.hostname}
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/cloudflared access tcp --hostname ${c.hostname} --url localhost:${port}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target`,
      },
      {
        id: 'launchd',
        title: 'LaunchAgent (macOS)',
        description: `Guárdalo en ~/Library/LaunchAgents/com.posterndb.${c.database}.plist y ejecuta: launchctl load ~/Library/LaunchAgents/com.posterndb.${c.database}.plist`,
        language: 'xml',
        code: `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.posterndb.${c.database}</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/cloudflared</string>
    <string>access</string><string>tcp</string>
    <string>--hostname</string><string>${c.hostname}</string>
    <string>--url</string><string>localhost:${port}</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>`,
      },
    )
  }

  out.push(
    {
      id: 'spring',
      title: 'Spring Boot',
      description: 'application.properties',
      language: 'properties',
      code: `spring.datasource.url=${jdbc}
spring.datasource.username=${c.username}
spring.datasource.password=${pass}`,
    },
    {
      id: 'node',
      title: 'Node.js (pg)',
      description: 'npm install pg',
      language: 'javascript',
      code: `import pg from 'pg'

const pool = new pg.Pool({
  host: 'localhost',
  port: ${port},
  database: '${c.database}',
  user: '${c.username}',
  password: process.env.DB_PASSWORD, // ${pass === PLACEHOLDER ? PLACEHOLDER : 'la contraseña de arriba'}
})`,
    },
    {
      id: 'python',
      title: 'Python (psycopg)',
      description: 'pip install "psycopg[binary]"',
      language: 'python',
      code: `import psycopg

conn = psycopg.connect(
    host="localhost", port=${port}, dbname="${c.database}",
    user="${c.username}", password="${pass}",
)`,
    },
    {
      id: 'dotnet',
      title: '.NET (Npgsql)',
      description: 'Cadena de conexión',
      language: 'text',
      code: `Host=localhost;Port=${port};Database=${c.database};Username=${c.username};Password=${pass}`,
    },
    {
      id: 'uri',
      title: 'URI (DBeaver, psql, TablePlus)',
      description: 'Pégala como URL de conexión.',
      language: 'text',
      code: uri,
    },
  )
  return out
}
