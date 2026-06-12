import { useEffect, useRef, useState } from 'react'
import { Folder, File as FileIcon, Upload, FolderPlus, Pencil, Trash2, Download, ChevronRight, Home, Save } from 'lucide-react'
import { api, formatBytes, formatDate } from '../api'
import type { FileEntry } from '../types'
import { Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Spinner, Empty, ErrorBanner, toast } from '../components/ui'

interface ListResponse {
  path: string
  entries: FileEntry[]
}

const TEXT_EXT = /\.(txt|html?|css|js|jsx|tsx?|json|xml|ya?ml|md|php|py|rb|sh|conf|ini|env|sql|log|htaccess)$/i

export default function Files() {
  const [path, setPath] = useState('/')
  const [data, setData] = useState<ListResponse | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [mkdirOpen, setMkdirOpen] = useState(false)
  const [newDir, setNewDir] = useState('')
  const [renaming, setRenaming] = useState<FileEntry | null>(null)
  const [newName, setNewName] = useState('')
  const [editing, setEditing] = useState<string | null>(null) // file path being edited
  const [content, setContent] = useState('')
  const [busy, setBusy] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const load = (p: string) => {
    setLoading(true)
    setError('')
    api
      .get<ListResponse>(`/api/files?path=${encodeURIComponent(p)}`)
      .then((d) => {
        setData(d)
        setPath(d.path)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => load('/'), [])

  const join = (name: string) => (path === '/' ? `/${name}` : `${path}/${name}`)

  const open = (entry: FileEntry) => {
    if (entry.is_dir) {
      load(join(entry.name))
    } else if (TEXT_EXT.test(entry.name) || entry.size < 256 * 1024) {
      api
        .get<{ content: string }>(`/api/files/content?path=${encodeURIComponent(join(entry.name))}`)
        .then((d) => {
          setContent(d.content)
          setEditing(join(entry.name))
        })
        .catch((e: Error) => toast(e.message, 'err'))
    } else {
      window.open(`/api/files/download?path=${encodeURIComponent(join(entry.name))}`)
    }
  }

  const saveFile = async () => {
    if (editing === null) return
    setBusy(true)
    try {
      await api.post('/api/files/content', { path: editing, content })
      toast('File saved')
      setEditing(null)
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const mkdir = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await api.post('/api/files/mkdir', { path: join(newDir) })
      toast('Folder created')
      setMkdirOpen(false)
      setNewDir('')
      load(path)
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const rename = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!renaming) return
    try {
      await api.post('/api/files/rename', { from: join(renaming.name), to: join(newName) })
      toast('Renamed')
      setRenaming(null)
      load(path)
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const remove = async (entry: FileEntry) => {
    if (!confirm(`Delete ${entry.is_dir ? 'folder' : 'file'} "${entry.name}"${entry.is_dir ? ' and everything in it' : ''}?`))
      return
    try {
      await api.post('/api/files/delete', { path: join(entry.name) })
      toast('Deleted')
      load(path)
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const upload = async (files: FileList | null) => {
    if (!files?.length) return
    for (const file of Array.from(files)) {
      const form = new FormData()
      form.append('path', path)
      form.append('file', file)
      const res = await fetch('/api/files/upload', { method: 'POST', body: form })
      if (!res.ok) {
        const d = await res.json().catch(() => ({ error: 'upload failed' }))
        toast(`${file.name}: ${(d as { error?: string }).error}`, 'err')
      }
    }
    toast('Upload complete')
    load(path)
  }

  const crumbs = path.split('/').filter(Boolean)

  return (
    <div>
      <PageHeader
        title="File Manager"
        subtitle="Your web space"
        actions={
          <>
            <input ref={fileInput} type="file" multiple hidden onChange={(e) => upload(e.target.files)} />
            <Btn variant="secondary" onClick={() => setMkdirOpen(true)}>
              <FolderPlus size={16} /> New Folder
            </Btn>
            <Btn onClick={() => fileInput.current?.click()}>
              <Upload size={16} /> Upload
            </Btn>
          </>
        }
      />
      <ErrorBanner message={error} />

      <div className="flex items-center gap-1 text-sm text-slate-500 mb-3 flex-wrap">
        <button onClick={() => load('/')} className="hover:text-brand-600 cursor-pointer">
          <Home size={15} />
        </button>
        {crumbs.map((c, i) => (
          <span key={i} className="flex items-center gap-1">
            <ChevronRight size={13} className="text-slate-300" />
            <button
              className="hover:text-brand-600 cursor-pointer"
              onClick={() => load('/' + crumbs.slice(0, i + 1).join('/'))}
            >
              {c}
            </button>
          </span>
        ))}
      </div>

      <Card>
        {loading ? (
          <Spinner />
        ) : !data?.entries.length ? (
          <Empty title="Empty folder" />
        ) : (
          <Table head={['Name', 'Size', 'Permissions', 'Modified', '']}>
            {data.entries.map((entry) => (
              <tr key={entry.name} className="hover:bg-slate-50/60">
                <Td>
                  <button
                    className="flex items-center gap-2 font-medium text-slate-700 hover:text-brand-600 cursor-pointer"
                    onClick={() => open(entry)}
                  >
                    {entry.is_dir ? (
                      <Folder size={16} className="text-brand-500 fill-brand-100" />
                    ) : (
                      <FileIcon size={16} className="text-slate-400" />
                    )}
                    {entry.name}
                  </button>
                </Td>
                <Td className="text-slate-500">{entry.is_dir ? '—' : formatBytes(entry.size)}</Td>
                <Td className="font-mono text-xs text-slate-500">{entry.mode}</Td>
                <Td className="text-slate-500">{formatDate(entry.mod_time)}</Td>
                <Td className="text-right whitespace-nowrap">
                  {!entry.is_dir && (
                    <a
                      href={`/api/files/download?path=${encodeURIComponent(join(entry.name))}`}
                      className="inline-block text-slate-400 hover:text-brand-600 mr-3"
                    >
                      <Download size={15} />
                    </a>
                  )}
                  <button
                    className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer"
                    onClick={() => {
                      setRenaming(entry)
                      setNewName(entry.name)
                    }}
                  >
                    <Pencil size={15} />
                  </button>
                  <button className="text-slate-400 hover:text-red-600 cursor-pointer" onClick={() => remove(entry)}>
                    <Trash2 size={15} />
                  </button>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={mkdirOpen} title="New Folder" onClose={() => setMkdirOpen(false)}>
        <form onSubmit={mkdir}>
          <Field label="Folder name">
            <Input value={newDir} onChange={(e) => setNewDir(e.target.value)} required />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setMkdirOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit">Create</Btn>
          </div>
        </form>
      </Modal>

      <Modal open={!!renaming} title={`Rename ${renaming?.name ?? ''}`} onClose={() => setRenaming(null)}>
        <form onSubmit={rename}>
          <Field label="New name">
            <Input value={newName} onChange={(e) => setNewName(e.target.value)} required />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setRenaming(null)}>
              Cancel
            </Btn>
            <Btn type="submit">Rename</Btn>
          </div>
        </form>
      </Modal>

      <Modal open={editing !== null} title={`Edit — ${editing ?? ''}`} onClose={() => setEditing(null)} wide>
        <textarea
          className="w-full h-96 font-mono text-xs border border-slate-300 rounded-md p-3 focus:outline-none focus:ring-2 focus:ring-brand-500/40"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          spellCheck={false}
        />
        <div className="flex justify-end gap-2 mt-3">
          <Btn variant="secondary" onClick={() => setEditing(null)}>
            Cancel
          </Btn>
          <Btn onClick={saveFile} disabled={busy}>
            <Save size={15} /> Save
          </Btn>
        </div>
      </Modal>
    </div>
  )
}
