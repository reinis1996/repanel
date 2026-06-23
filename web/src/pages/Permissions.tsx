import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '../api'
import { Btn, Card, PageHeader, Spinner, toast, ModuleChecklist } from '../components/ui'

interface PermData {
  modules: { key: string; label: string }[]
  user: string[]
  reseller: string[]
}

export default function Permissions() {
  const [data, setData] = useState<PermData | null>(null)
  const [user, setUser] = useState<string[]>([])
  const [reseller, setReseller] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .get<PermData>('/api/permissions')
      .then((d) => {
        setData(d)
        setUser(d.user)
        setReseller(d.reseller)
      })
      .catch(() => setData(null))
  }, [])

  const save = async () => {
    setBusy(true)
    try {
      await api.post('/api/permissions', { user, reseller })
      toast('Default permissions saved')
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  if (!data) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Permissions"
        subtitle="Default module access applied to newly created accounts in each group"
      />
      <div className="grid md:grid-cols-2 gap-4 max-w-3xl">
        <Card title="User group default">
          <ModuleChecklist all={data.modules} selected={user} onChange={setUser} />
        </Card>
        <Card title="Reseller group default">
          <ModuleChecklist all={data.modules} selected={reseller} onChange={setReseller} />
        </Card>
      </div>
      <div className="max-w-3xl flex justify-end mt-4">
        <Btn onClick={save} disabled={busy}>
          <Save size={15} /> Save defaults
        </Btn>
      </div>
      <p className="text-xs text-slate-400 mt-3 max-w-3xl">
        These apply to new accounts only. Existing users keep their current access — edit a user on the Users page to
        change theirs. Administrators always have full access.
      </p>
    </div>
  )
}
