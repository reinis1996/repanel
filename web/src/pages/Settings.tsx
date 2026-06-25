import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '../api'
import { Btn, Card, PageHeader, Field, Input, Select, Spinner, toast } from '../components/ui'

type SettingsMap = Record<string, string>

const FIELDS: { key: string; label: string; hint: string; placeholder?: string; options?: [string, string][] }[] = [
  { key: 'server_ip', label: 'Server public IP', hint: 'Default A record for new DNS zones', placeholder: '203.0.113.10' },
  { key: 'panel_hostname', label: 'Panel hostname', hint: 'FQDN of this server', placeholder: 'panel.example.com' },
  { key: 'ns1', label: 'Primary nameserver', hint: 'Used in SOA/NS records', placeholder: 'ns1.example.com' },
  { key: 'ns2', label: 'Secondary nameserver', hint: 'Optional — added as a second NS record on every zone', placeholder: 'ns2.example.com' },
  {
    key: 'slave_dns',
    label: 'Secondary DNS server IPs',
    hint: 'Comma-separated IPs allowed to transfer (AXFR) and notified on change',
    placeholder: '198.51.100.2, 203.0.113.9',
  },
  { key: 'admin_email', label: 'Admin email', hint: "Used for Let's Encrypt and zone SOA", placeholder: 'admin@example.com' },
  {
    key: 'resolver_dns',
    label: 'System DNS resolvers',
    hint: "The server's own DNS resolvers (writes /etc/resolv.conf). Leave blank to keep the DHCP-provided ones.",
    placeholder: '1.1.1.1, 8.8.8.8',
  },
  {
    key: 'backup_schedule',
    label: 'Automatic backups',
    hint: 'Runs around 03:00 server time for every active account',
    options: [
      ['', 'Disabled'],
      ['daily', 'Daily'],
      ['weekly', 'Weekly (Sunday)'],
    ],
  },
  { key: 'backup_keep', label: 'Backups to keep per account', hint: 'Older archives are deleted automatically (default 5)', placeholder: '5' },
]

// Alerting settings get their own card (with a test button).
const ALERT_FIELDS: { key: string; label: string; hint: string; placeholder?: string; options?: [string, string][] }[] = [
  {
    key: 'alerts_enabled',
    label: 'Alerts',
    hint: 'Notify on disk-full, a service going down, certificate expiry and backup failures',
    options: [['', 'Disabled'], ['1', 'Enabled']],
  },
  { key: 'alert_email', label: 'Alert email', hint: 'Where to send alert emails (via the local mail server)', placeholder: 'ops@example.com' },
  { key: 'alert_webhook', label: 'Alert webhook', hint: 'Optional — alerts are POSTed here as JSON', placeholder: 'https://hooks.example.com/…' },
  { key: 'alert_disk_pct', label: 'Disk alert threshold (%)', hint: 'Alert when disk usage reaches this (default 90)', placeholder: '90' },
  { key: 'alert_cert_days', label: 'Certificate expiry warning (days)', hint: 'Alert this many days before a cert expires (default 14)', placeholder: '14' },
]

export default function Settings() {
  const [values, setValues] = useState<SettingsMap | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.get<SettingsMap>('/api/settings').then(setValues).catch(() => setValues({}))
  }, [])

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!values) return
    setBusy(true)
    try {
      await api.post('/api/settings', values)
      toast('Settings saved')
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  if (!values) return <Spinner />

  return (
    <div>
      <PageHeader title="Settings" subtitle="Server-wide panel configuration" />
      <Card className="max-w-2xl">
        <form onSubmit={save}>
          {FIELDS.map((f) => (
            <Field key={f.key} label={f.label} hint={f.hint}>
              {f.options ? (
                <Select value={values[f.key] ?? ''} onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}>
                  {f.options.map(([v, label]) => (
                    <option key={v} value={v}>
                      {label}
                    </option>
                  ))}
                </Select>
              ) : (
                <Input
                  value={values[f.key] ?? ''}
                  placeholder={f.placeholder}
                  onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                />
              )}
            </Field>
          ))}
          <div className="flex justify-end">
            <Btn type="submit" disabled={busy}>
              <Save size={15} /> Save settings
            </Btn>
          </div>
        </form>
      </Card>

      <Card className="max-w-2xl mt-4" title="Branding (white-label)">
        <form onSubmit={save}>
          <Field label="Panel name" hint="Shown in the sidebar, login screen and browser title (default RePanel)">
            <Input value={values.brand_name ?? ''} placeholder="RePanel" maxLength={40} onChange={(e) => setValues({ ...values, brand_name: e.target.value })} />
          </Field>
          <Field label="Accent color" hint="Hex color for buttons and highlights — leave blank for the default blue">
            <div className="flex items-center gap-2">
              <input
                type="color"
                value={values.brand_color || '#1a6fd4'}
                onChange={(e) => setValues({ ...values, brand_color: e.target.value })}
                className="h-9 w-10 rounded border border-slate-300 cursor-pointer bg-white"
              />
              <Input value={values.brand_color ?? ''} placeholder="#1a6fd4" onChange={(e) => setValues({ ...values, brand_color: e.target.value })} />
            </div>
          </Field>
          <Field label="Logo URL" hint="Optional image shown instead of the name (an http(s) URL or absolute path)">
            <Input value={values.brand_logo ?? ''} placeholder="https://example.com/logo.svg" onChange={(e) => setValues({ ...values, brand_logo: e.target.value })} />
          </Field>
          <div className="flex justify-end">
            <Btn type="submit" disabled={busy}>
              <Save size={15} /> Save settings
            </Btn>
          </div>
        </form>
      </Card>

      <Card className="max-w-2xl mt-4" title="Alerts & notifications">
        <form onSubmit={save}>
          {ALERT_FIELDS.map((f) => (
            <Field key={f.key} label={f.label} hint={f.hint}>
              {f.options ? (
                <Select value={values[f.key] ?? ''} onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}>
                  {f.options.map(([v, label]) => (
                    <option key={v} value={v}>{label}</option>
                  ))}
                </Select>
              ) : (
                <Input
                  value={values[f.key] ?? ''}
                  placeholder={f.placeholder}
                  onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                />
              )}
            </Field>
          ))}
          <div className="flex justify-between">
            <Btn
              type="button"
              variant="secondary"
              disabled={busy}
              onClick={async () => {
                try {
                  await api.post('/api/alerts/test')
                  toast('Test notification sent')
                } catch (err) {
                  toast((err as Error).message, 'err')
                }
              }}
            >
              Send test notification
            </Btn>
            <Btn type="submit" disabled={busy}>
              <Save size={15} /> Save settings
            </Btn>
          </div>
        </form>
      </Card>
    </div>
  )
}
