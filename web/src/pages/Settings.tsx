import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '../api'
import { Btn, Card, PageHeader, Field, Input, Select, Spinner, toast } from '../components/ui'

type SettingsMap = Record<string, string>

const FIELDS: { key: string; label: string; hint: string; placeholder?: string; options?: [string, string][] }[] = [
  { key: 'server_ip', label: 'Server public IP', hint: 'Default A record for new DNS zones', placeholder: '203.0.113.10' },
  { key: 'panel_hostname', label: 'Panel hostname', hint: 'FQDN of this server', placeholder: 'panel.example.com' },
  { key: 'ns1', label: 'Primary nameserver', hint: 'Used in SOA/NS records', placeholder: 'ns1.example.com' },
  { key: 'ns2', label: 'Secondary nameserver', hint: 'Optional', placeholder: 'ns2.example.com' },
  { key: 'admin_email', label: 'Admin email', hint: "Used for Let's Encrypt and zone SOA", placeholder: 'admin@example.com' },
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
    </div>
  )
}
