import { useEffect, useRef, useState } from 'react'
import Editor from '@monaco-editor/react'
import {
  Alert, AppBar, Avatar, Box, Button, Card, CardContent, Chip, CircularProgress,
  Dialog, DialogActions, DialogContent, DialogTitle, Divider, Drawer, Fab, FormControl,
  IconButton, InputAdornment, InputLabel, List, ListItemButton, ListItemIcon,
  ListItemText, MenuItem, Select, Snackbar, Stack, Switch, Table, TableBody, TableCell,
  TableContainer, TableHead, TableRow, TextField, Toolbar, Tooltip, Typography,
  useMediaQuery,
} from '@mui/material'
import {
  AddRounded, BoltRounded, BugReportRounded, ChevronRightRounded, CloudDownloadRounded,
  DashboardRounded, DeleteOutlineRounded, DeviceHubRounded, DnsRounded, EditRounded,
  FilterAltRounded, HubRounded, LanRounded, LockResetRounded, MenuRounded, MemoryRounded,
  NetworkCheckRounded, PauseRounded, PlayArrowRounded, PowerSettingsNewRounded,
  RefreshRounded, RestartAltRounded, RouteRounded, SearchRounded, SettingsRounded,
  ShieldRounded, SpeedRounded, StopRounded, StorageRounded, SwapVertRounded,
  TerminalRounded, TimelineRounded, TuneRounded, UploadFileRounded, WifiRounded,
} from '@mui/icons-material'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router'
import { APIError, Me, Profile, Status, api, json, wsURL } from './api'

const drawerWidth = 224

const nav = [
  ['首页', '/', DashboardRounded], ['代理', '/proxies', WifiRounded],
  ['订阅', '/profiles', StorageRounded], ['连接', '/connections', LanRounded],
  ['规则', '/rules', RouteRounded], ['日志', '/logs', TerminalRounded],
  ['测试', '/test', NetworkCheckRounded], ['设置', '/settings', SettingsRounded],
] as const

export default function App() {
  const me = useQuery<Me>({ queryKey: ['me'], queryFn: () => api('/auth/me'), retry: false })
  if (me.isLoading) return <CenteredProgress />
  if (me.error instanceof APIError && me.error.status === 401) return <LoginScreen />
  if (me.error) return <FatalState message={(me.error as Error).message} onRetry={() => me.refetch()} />
  return <Shell me={me.data!} />
}

function LoginScreen() {
  const qc = useQueryClient()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const login = useMutation({
    mutationFn: () => api('/auth/login', json('POST', { username, password })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['me'] }),
  })
  return <Box className="login-stage">
    <Box className="login-orbit orbit-one" /><Box className="login-orbit orbit-two" />
    <Card className="login-card">
      <CardContent sx={{ p: { xs: 3, sm: 4 } }}>
        <Box className="brand-mark large"><HubRounded /></Box>
        <Typography variant="overline" color="primary">MIHOMO CONTROL PLANE</Typography>
        <Typography variant="h4" sx={{ mt: .5 }}>Clash Web</Typography>
        <Typography color="text.secondary" sx={{ mt: 1, mb: 3 }}>登录服务器控制台，管理代理内核与网络路径。</Typography>
        <Stack spacing={2} component="form" onSubmit={e => { e.preventDefault(); login.mutate() }}>
          <TextField label="用户名" value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" />
          <TextField label="密码" value={password} type="password" autoFocus onChange={e => setPassword(e.target.value)} autoComplete="current-password" />
          {login.error && <Alert severity="error">{(login.error as Error).message}</Alert>}
          <Button size="large" variant="contained" type="submit" disabled={!password || login.isPending}>{login.isPending ? '正在验证…' : '进入控制台'}</Button>
        </Stack>
        <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 2.5 }}>首次密码位于服务器的 bootstrap-password 文件中。</Typography>
      </CardContent>
    </Card>
  </Box>
}

function Shell({ me }: { me: Me }) {
  const mobile = useMediaQuery('(max-width:900px)')
  const [open, setOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const status = useQuery<Status>({ queryKey: ['status'], queryFn: () => api('/status'), refetchInterval: 5000 })
  const [traffic, setTraffic] = useState<TrafficPoint[]>([])
  useLiveStream('traffic', status.data?.coreOnline === true, data => {
    setTraffic(current => [...current.slice(-34), { up: Number(data.up || 0), down: Number(data.down || 0), upTotal: Number(data.upTotal || 0), downTotal: Number(data.downTotal || 0) }])
  })
  const title = nav.find(([, href]) => location.pathname === href)?.[0] || 'Clash Web'
  const logout = async () => { await api('/auth/logout', { method: 'POST' }); await qc.invalidateQueries({ queryKey: ['me'] }) }
  const drawer = <Box className="sidebar">
    <Box className="brand"><Box className="brand-mark"><HubRounded /></Box><Box><Typography className="brand-name">Clash Web</Typography><Typography variant="caption">SERVER EDITION</Typography></Box></Box>
    <List className="nav-list">{nav.map(([label, href, Icon]) => <ListItemButton key={href} selected={location.pathname === href} onClick={() => { navigate(href); setOpen(false) }}>
      <ListItemIcon><Icon /></ListItemIcon><ListItemText primary={label} /><ChevronRightRounded className="nav-arrow" />
    </ListItemButton>)}</List>
    <TelemetryRail points={traffic} online={status.data?.coreOnline === true} />
  </Box>
  return <Box className="app-shell">
    {mobile ? <Drawer open={open} onClose={() => setOpen(false)}>{drawer}</Drawer> : <Drawer variant="permanent" sx={{ width: drawerWidth, '& .MuiDrawer-paper': { width: drawerWidth } }}>{drawer}</Drawer>}
    <AppBar position="fixed" color="transparent" elevation={0} sx={{ ml: mobile ? 0 : `${drawerWidth}px`, width: mobile ? '100%' : `calc(100% - ${drawerWidth}px)` }}>
      <Toolbar><IconButton onClick={() => setOpen(true)} sx={{ display: mobile ? 'inline-flex' : 'none', mr: 1 }}><MenuRounded /></IconButton>
        <Box sx={{ flex: 1 }}><Typography variant="overline" color="text.secondary">CONTROL / {location.pathname.slice(1).toUpperCase() || 'OVERVIEW'}</Typography><Typography variant="h6" lineHeight={1}>{title}</Typography></Box>
        <Chip size="small" className={status.data?.coreOnline ? 'status-chip online' : 'status-chip'} icon={<BoltRounded />} label={status.data?.coreOnline ? '内核运行中' : '内核离线'} />
        <Tooltip title="刷新"><IconButton onClick={() => status.refetch()}><RefreshRounded /></IconButton></Tooltip>
        <Tooltip title={`${me.username} · 退出`}><IconButton onClick={logout}><Avatar sx={{ width: 30, height: 30, bgcolor: 'secondary.main', fontSize: 13 }}>AD</Avatar></IconButton></Tooltip>
      </Toolbar>
    </AppBar>
    <Box component="main" className="main" sx={{ ml: mobile ? 0 : `${drawerWidth}px` }}>
      {me.mustChangePassword && <Alert severity="warning" sx={{ mb: 2 }}>当前仍在使用初始密码。请在设置中立即修改。</Alert>}
      <Routes>
        <Route path="/" element={<HomePage status={status.data} traffic={traffic} />} />
        <Route path="/proxies" element={<ProxiesPage online={status.data?.coreOnline} />} />
        <Route path="/profiles" element={<ProfilesPage />} />
        <Route path="/connections" element={<ConnectionsPage online={status.data?.coreOnline} />} />
        <Route path="/rules" element={<RulesPage online={status.data?.coreOnline} />} />
        <Route path="/logs" element={<LogsPage online={status.data?.coreOnline} />} />
        <Route path="/test" element={<TestPage online={status.data?.coreOnline} />} />
        <Route path="/settings" element={<SettingsPage status={status.data} />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Box>
  </Box>
}

type TrafficPoint = { up: number; down: number; upTotal: number; downTotal: number }

function HomePage({ status, traffic }: { status?: Status; traffic: TrafficPoint[] }) {
  const qc = useQueryClient()
  const action = useMutation({ mutationFn: (name: string) => api(`/core/${name}`, { method: 'POST' }), onSuccess: () => qc.invalidateQueries({ queryKey: ['status'] }) })
  const last = traffic.at(-1) || { up: 0, down: 0, upTotal: 0, downTotal: 0 }
  const config = status?.config || {}
  return <Stack spacing={2.2}>
    <Box className="hero-grid">
      <Card className="hero-status"><CardContent>
        <Stack direction="row" justifyContent="space-between" alignItems="flex-start"><Box><Typography variant="overline" color="text.secondary">CORE ENGINE</Typography><Typography variant="h4" sx={{ mt: .5 }}>{status?.coreOnline ? '路径已接管' : '等待配置'}</Typography></Box><Box className={`pulse ${status?.coreOnline ? 'live' : ''}`} /></Stack>
        <Typography color="text.secondary" sx={{ mt: 1 }}>{status?.coreOnline ? `mihomo ${status.core?.version || ''} 正在处理服务器流量。` : '导入并启用一个订阅，启动 mihomo 内核。'}</Typography>
        <Stack direction="row" spacing={1} sx={{ mt: 3 }}><Button variant="contained" startIcon={<RestartAltRounded />} onClick={() => action.mutate('restart')} disabled={!status?.helperOnline}>重启内核</Button><Button variant="outlined" startIcon={<StopRounded />} onClick={() => action.mutate('stop')} disabled={!status?.coreOnline}>停止</Button></Stack>
      </CardContent></Card>
      <Card className="traffic-card"><CardContent><Box className="metric-label"><SwapVertRounded /> LIVE TRAFFIC</Box><Stack direction="row" spacing={4} sx={{ mt: 2 }}><Metric label="上传" value={rate(last.up)} color="#f2a64a"/><Metric label="下载" value={rate(last.down)} color="#53c8f2"/></Stack><TrafficChart points={traffic} /></CardContent></Card>
    </Box>
    <Box className="stat-grid">
      <StatCard icon={<TuneRounded />} label="运行模式" value={String(config.mode || 'Rule')} foot="规则引擎" />
      <StatCard icon={<ShieldRounded />} label="TUN" value={config.tun?.enable ? 'Enabled' : 'Disabled'} foot={config.tun?.stack || 'system stack'} />
      <StatCard icon={<MemoryRounded />} label="内核进程" value={status?.helper?.pid ? `PID ${status.helper.pid}` : '—'} foot={status?.helperOnline ? 'helper connected' : 'helper offline'} />
      <StatCard icon={<TimelineRounded />} label="累计下载" value={bytes(last.downTotal)} foot={`上传 ${bytes(last.upTotal)}`} />
    </Box>
    {!status?.helperOnline && <EmptyGuide title="管理助手未连接" detail="检查 clash-web-helper.service 是否正在运行，以及 Unix Socket 权限。" />}
  </Stack>
}

function ProxiesPage({ online }: { online?: boolean }) {
  const [query, setQuery] = useState('')
  const qc = useQueryClient()
  const proxies = useQuery<any>({ queryKey: ['proxies'], queryFn: () => api('/proxies'), enabled: !!online, refetchInterval: 10000 })
  const select = useMutation({ mutationFn: ({ group, name }: { group: string; name: string }) => api(`/proxies/${encodeURIComponent(group)}`, json('PUT', { name })), onSuccess: () => qc.invalidateQueries({ queryKey: ['proxies'] }) })
  if (!online) return <OfflineState />
  if (proxies.isLoading) return <CenteredProgress />
  const records = proxies.data?.proxies || {}
  const groups = Object.values(records).filter((p: any) => Array.isArray(p.all)) as any[]
  return <Stack spacing={2}><PageTools title={`${groups.length} 个代理组`} query={query} setQuery={setQuery} action={<Button startIcon={<SpeedRounded />} onClick={() => qc.invalidateQueries({ queryKey: ['proxies'] })}>刷新状态</Button>} />
    {select.error && <Alert severity="error">{(select.error as Error).message}</Alert>}
    <Box className="proxy-grid">{groups.filter(g => g.name.toLowerCase().includes(query.toLowerCase()) || g.all.some((n: string) => n.toLowerCase().includes(query.toLowerCase()))).map(group => <Card key={group.name} className="proxy-group"><CardContent>
      <Stack direction="row" justifyContent="space-between"><Box><Typography variant="h6">{group.name}</Typography><Typography variant="caption" color="text.secondary">{group.type} · {group.all.length} nodes</Typography></Box><Chip size="small" label={group.now || '未选择'} color="primary" variant="outlined" /></Stack>
      {String(group.type).toLowerCase() !== 'selector' && <Alert severity="info" variant="outlined" sx={{ mt: 2 }}>该组由 mihomo 自动选择，节点状态仅供查看。</Alert>}
      <Divider sx={{ my: 2 }} /><Box className="node-list">{group.all.map((name: string) => { const node = records[name] || {}; const delay = node.history?.at(-1)?.delay; const selectable = String(group.type).toLowerCase() === 'selector'; return <Button key={name} disabled={!selectable || select.isPending} className={group.now === name ? 'node active' : 'node'} onClick={() => select.mutate({ group: group.name, name })}><span>{name}</span><small>{delay ? `${delay} ms` : node.type || '—'}</small></Button> })}</Box>
    </CardContent></Card>)}</Box>
    {!groups.length && <EmptyGuide title="没有代理组" detail="当前配置没有可选择的策略组。" />}
  </Stack>
}

function ProfilesPage() {
  const qc = useQueryClient(); const [open, setOpen] = useState(false); const [editing, setEditing] = useState<Profile | null>(null); const [message, setMessage] = useState('')
  const profiles = useQuery<{ profiles: Profile[] }>({ queryKey: ['profiles'], queryFn: () => api('/profiles/') })
  const activate = useMutation({ mutationFn: (id: number) => api(`/profiles/${id}/activate`, { method: 'POST' }), onSuccess: () => { qc.invalidateQueries({ queryKey: ['profiles'] }); qc.invalidateQueries({ queryKey: ['status'] }); setMessage('配置已验证并启用') } })
  const remove = useMutation({ mutationFn: (id: number) => api(`/profiles/${id}`, { method: 'DELETE' }), onSuccess: () => qc.invalidateQueries({ queryKey: ['profiles'] }) })
  const refresh = useMutation({ mutationFn: (id: number) => api(`/profiles/${id}/refresh`, { method: 'POST' }), onSuccess: () => { qc.invalidateQueries({ queryKey: ['profiles'] }); setMessage('订阅已更新') } })
  const loadEditor = async (p: Profile) => setEditing(await api(`/profiles/${p.id}`))
  return <><Stack spacing={2}><PageTools title="配置与订阅" action={<Button variant="contained" startIcon={<AddRounded />} onClick={() => setOpen(true)}>新建订阅</Button>} />
    <Box className="profile-grid">{(profiles.data?.profiles || []).map(p => <Card key={p.id} className={p.active ? 'profile-card active' : 'profile-card'}><CardContent>
      <Stack direction="row" justifyContent="space-between" alignItems="flex-start"><Box className="profile-icon"><DnsRounded /></Box><Chip size="small" label={p.active ? 'ACTIVE' : p.source.toUpperCase()} color={p.active ? 'primary' : 'default'} /></Stack>
      <Typography variant="h6" sx={{ mt: 2 }}>{p.name}</Typography><Typography variant="body2" color="text.secondary" noWrap>{p.url || '本地 YAML 配置'}</Typography><Typography variant="caption" color="text.secondary">更新于 {displayTime(p.updatedAt)}</Typography>
      <Divider sx={{ my: 2 }} /><Stack direction="row" spacing={.5}><Tooltip title="启用"><IconButton color={p.active ? 'primary' : 'default'} onClick={() => activate.mutate(p.id)}><PlayArrowRounded /></IconButton></Tooltip>{p.url && <Tooltip title="更新"><IconButton onClick={() => refresh.mutate(p.id)}><CloudDownloadRounded /></IconButton></Tooltip>}<Tooltip title="编辑"><IconButton onClick={() => loadEditor(p)}><EditRounded /></IconButton></Tooltip><Box flex={1}/><Tooltip title="删除"><IconButton color="error" onClick={() => remove.mutate(p.id)}><DeleteOutlineRounded /></IconButton></Tooltip></Stack>
    </CardContent></Card>)}</Box>
    {!profiles.data?.profiles.length && <EmptyGuide title="还没有订阅" detail="从 URL 导入，或粘贴一份 mihomo YAML 配置开始使用。" action={<Button variant="contained" onClick={() => setOpen(true)}>添加第一个配置</Button>} />}</Stack>
    <ProfileDialog open={open} onClose={() => setOpen(false)} onSaved={() => { setOpen(false); qc.invalidateQueries({ queryKey: ['profiles'] }) }} />
    <EditorDialog profile={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); qc.invalidateQueries({ queryKey: ['profiles'] }) }} />
    <Snackbar open={!!message} autoHideDuration={2600} onClose={() => setMessage('')} message={message} />
  </>
}

function ConnectionsPage({ online }: { online?: boolean }) {
  const [query, setQuery] = useState(''); const qc = useQueryClient(); const connections = useQuery<any>({ queryKey: ['connections'], queryFn: () => api('/connections'), enabled: !!online, refetchInterval: 2000 })
  const close = useMutation({ mutationFn: (id?: string) => api(id ? `/connections/${id}` : '/connections', { method: 'DELETE' }), onSuccess: () => qc.invalidateQueries({ queryKey: ['connections'] }) })
  if (!online) return <OfflineState />; const rows = (connections.data?.connections || []).filter((c: any) => JSON.stringify(c).toLowerCase().includes(query.toLowerCase()))
  return <Stack spacing={2}><PageTools title={`${rows.length} 个活动连接`} query={query} setQuery={setQuery} action={<Button color="error" startIcon={<DeleteOutlineRounded />} onClick={() => close.mutate(undefined)}>关闭全部</Button>} />
    <Card><TableContainer><Table size="small"><TableHead><TableRow><TableCell>目标</TableCell><TableCell>网络</TableCell><TableCell>规则</TableCell><TableCell>链路</TableCell><TableCell align="right">流量</TableCell><TableCell /></TableRow></TableHead><TableBody>{rows.map((c: any) => <TableRow key={c.id} hover><TableCell><Typography variant="body2">{c.metadata?.host || c.metadata?.destinationIP}</Typography><Typography variant="caption" color="text.secondary">{c.metadata?.destinationPort}</Typography></TableCell><TableCell>{c.metadata?.network}</TableCell><TableCell>{c.rule}</TableCell><TableCell>{(c.chains || []).join(' → ')}</TableCell><TableCell align="right" className="mono">{bytes((c.upload || 0) + (c.download || 0))}</TableCell><TableCell><IconButton size="small" onClick={() => close.mutate(c.id)}><DeleteOutlineRounded fontSize="small" /></IconButton></TableCell></TableRow>)}</TableBody></Table></TableContainer></Card>
    {!rows.length && <EmptyGuide title="当前没有活动连接" detail="新连接建立后会自动出现在这里。" />}
  </Stack>
}

function RulesPage({ online }: { online?: boolean }) {
  const [query, setQuery] = useState(''); const rules = useQuery<any>({ queryKey: ['rules'], queryFn: () => api('/rules'), enabled: !!online })
  if (!online) return <OfflineState />; const rows = (rules.data?.rules || []).filter((r: any) => JSON.stringify(r).toLowerCase().includes(query.toLowerCase()))
  return <Stack spacing={2}><PageTools title={`${rows.length} 条路由规则`} query={query} setQuery={setQuery} /><Card><TableContainer><Table size="small"><TableHead><TableRow><TableCell width="18%">类型</TableCell><TableCell>匹配条件</TableCell><TableCell width="25%">目标策略</TableCell></TableRow></TableHead><TableBody>{rows.map((r: any, i: number) => <TableRow key={i}><TableCell><Chip size="small" label={r.type} variant="outlined" /></TableCell><TableCell className="mono">{r.payload || 'MATCH'}</TableCell><TableCell>{r.proxy}</TableCell></TableRow>)}</TableBody></Table></TableContainer></Card></Stack>
}

function LogsPage({ online }: { online?: boolean }) {
  const [logs, setLogs] = useState<any[]>([]); const [paused, setPaused] = useState(false); const [level, setLevel] = useState('all')
  useLiveStream('logs', !!online && !paused, data => setLogs(current => [...current.slice(-499), data]))
  if (!online) return <OfflineState />; const filtered = logs.filter(l => level === 'all' || l.type === level)
  const exportLogs = () => { const blob = new Blob(filtered.map(l => JSON.stringify(l)).join('\n').split('\n').map(line => `${line}\n`), { type: 'application/x-ndjson' }); const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = `mihomo-${new Date().toISOString().slice(0, 10)}.ndjson`; link.click(); URL.revokeObjectURL(link.href) }
  return <Stack spacing={2}><PageTools title={`${filtered.length} 条实时日志`} action={<Stack direction="row" spacing={1}><FormControl size="small" sx={{ minWidth: 120 }}><InputLabel>等级</InputLabel><Select value={level} label="等级" onChange={e => setLevel(e.target.value)}><MenuItem value="all">全部</MenuItem><MenuItem value="debug">Debug</MenuItem><MenuItem value="info">Info</MenuItem><MenuItem value="warning">Warning</MenuItem><MenuItem value="error">Error</MenuItem></Select></FormControl><Button startIcon={paused ? <PlayArrowRounded /> : <PauseRounded />} onClick={() => setPaused(v => !v)}>{paused ? '继续' : '暂停'}</Button><Button onClick={exportLogs} disabled={!filtered.length}>导出</Button><Button onClick={() => setLogs([])}>清空</Button></Stack>} />
    <Card className="terminal"><Box className="terminal-head"><i/><i/><i/><Typography variant="caption">mihomo / live stream</Typography></Box><Box className="terminal-body">{filtered.map((l, i) => <div key={i} className={`log-line ${l.type || ''}`}><time>{new Date().toLocaleTimeString()}</time><b>{String(l.type || 'info').toUpperCase()}</b><span>{l.payload || l.message || JSON.stringify(l)}</span></div>)}{!filtered.length && <Typography color="text.secondary">等待内核日志…</Typography>}</Box></Card>
  </Stack>
}

function TestPage({ online }: { online?: boolean }) {
  const [url, setURL] = useState('http://cp.cloudflare.com/generate_204'); const [result, setResult] = useState(''); const [running, setRunning] = useState(false)
  const run = async () => { setRunning(true); const started = performance.now(); try { const proxies: any = await api('/proxies'); const group = Object.values(proxies.proxies || {}).find((p: any) => Array.isArray(p.all)) as any; if (!group) throw new Error('配置中没有代理组'); const data: any = await api(`/proxies/${encodeURIComponent(group.now || group.all[0])}/delay?url=${encodeURIComponent(url)}&timeout=8000`); setResult(`${data.delay} ms · ${group.now || group.all[0]}`) } catch (e) { setResult((e as Error).message) } finally { console.debug(performance.now()-started); setRunning(false) } }
  if (!online) return <OfflineState />
  return <Box className="test-layout"><Card className="test-panel"><CardContent><Box className="radar"><Box className="radar-sweep"/><NetworkCheckRounded /></Box><Typography variant="h5">路径连通性测试</Typography><Typography color="text.secondary" sx={{ mt: 1, mb: 3 }}>通过当前策略节点请求测试地址，检查握手和端到端延迟。</Typography><TextField fullWidth label="测试 URL" value={url} onChange={e => setURL(e.target.value)} /><Button fullWidth size="large" variant="contained" startIcon={<SpeedRounded />} onClick={run} disabled={running} sx={{ mt: 2 }}>{running ? '正在探测…' : '开始测试'}</Button>{result && <Alert severity={result.includes('ms') ? 'success' : 'error'} sx={{ mt: 2 }}>{result}</Alert>}</CardContent></Card>
    <Box><StatCard icon={<DnsRounded />} label="DNS" value="由 mihomo 处理" foot="遵循当前 DNS 配置"/><Box height={16}/><StatCard icon={<DeviceHubRounded />} label="测试路径" value="代理链路" foot="不使用浏览器本地网络"/></Box></Box>
}

function SettingsPage({ status }: { status?: Status }) {
  const qc = useQueryClient()
  const config = status?.config || {}
  const [network, setNetwork] = useState(() => settingsFromConfig(config))
  const [passwords, setPasswords] = useState({ current: '', password: '' })
  const [notice, setNotice] = useState('')
  const [currentConfig, setCurrentConfig] = useState<string | null>(null)
  const cores = useQuery<CoreList>({ queryKey: ['cores'], queryFn: () => api('/cores'), enabled: !!status?.helperOnline })

  useEffect(() => setNetwork(settingsFromConfig(config)), [status?.config])

  const saveCore = useMutation({
    mutationFn: () => api('/config', json('PATCH', {
      mode: network.mode,
      'allow-lan': network.allowLan,
      'bind-address': network.bindAddress || '*',
      ipv6: network.ipv6,
      'mixed-port': parsePort(network.mixedPort),
      port: parsePort(network.httpPort),
      'socks-port': parsePort(network.socksPort),
      'redir-port': parsePort(network.redirPort),
      'tproxy-port': parsePort(network.tproxyPort),
      tun: { ...(config.tun || {}), enable: network.tun },
      dns: {
        ...(config.dns || {}),
        enable: network.dnsEnabled,
        ipv6: network.dnsIPv6,
        listen: network.dnsListen,
        'enhanced-mode': network.dnsMode,
      },
    })),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['status'] }); setNotice('网络与 DNS 设置已持久化') },
  })
  const selectCore = useMutation({
    mutationFn: (id: string) => api('/cores/select', json('POST', { id })),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['cores'] }); qc.invalidateQueries({ queryKey: ['status'] }); setNotice('mihomo 内核已切换') },
  })
  const updateCore = useMutation({
    mutationFn: () => api('/cores/update', json('POST', { select: true })),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['cores'] }); qc.invalidateQueries({ queryKey: ['status'] }); setNotice('官方稳定版内核已校验、安装并启用') },
  })
  const updateGeoData = useMutation({
    mutationFn: () => api('/geodata/update', json('POST', {})),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['status'] }); setNotice('GeoData 已校验更新，内核已重启') },
  })
  const openCurrentConfig = async () => {
    try {
      const result = await api<{ content: string }>('/current-config')
      setCurrentConfig(result.content)
    } catch (error) { setNotice((error as Error).message) }
  }
  const saveCurrentConfig = async () => {
    if (currentConfig == null) return
    try {
      await api('/current-config', { method: 'PUT', body: currentConfig, headers: { 'Content-Type': 'application/yaml' } })
      setCurrentConfig(null)
      await qc.invalidateQueries({ queryKey: ['status'] })
      setNotice('当前配置已验证并重新加载')
    } catch (error) { setNotice((error as Error).message) }
  }
  const changePassword = useMutation({ mutationFn: () => api('/auth/password', json('POST', passwords)), onSuccess: () => { setPasswords({ current: '', password: '' }); qc.invalidateQueries({ queryKey: ['me'] }); setNotice('管理员密码已更新') } })
  const maintenanceError = selectCore.error || updateCore.error || updateGeoData.error || saveCore.error

  return <Stack spacing={2}>
    <Box className="settings-grid">
      <Card><CardContent>
        <SectionTitle icon={<LanRounded />} title="局域网与运行模式" detail="设置 Clash 监听范围、IPv6 与服务器 TUN。"/>
        <Divider sx={{ my: 2 }}/>
        <Stack spacing={2.5}>
          <FormControl fullWidth><InputLabel>代理模式</InputLabel><Select value={network.mode} label="代理模式" onChange={e => setNetwork(v => ({ ...v, mode: e.target.value }))}><MenuItem value="rule">Rule</MenuItem><MenuItem value="global">Global</MenuItem><MenuItem value="direct">Direct</MenuItem></Select></FormControl>
          <SettingSwitch title="允许局域网连接" detail="允许其他内网设备使用本机 Clash 端口" checked={network.allowLan} onChange={allowLan => setNetwork(v => ({ ...v, allowLan }))}/>
          <TextField label="绑定地址" value={network.bindAddress} disabled={!network.allowLan} helperText="* 表示所有网卡；关闭局域网连接时仅本机可用" onChange={e => setNetwork(v => ({ ...v, bindAddress: e.target.value }))}/>
          <SettingSwitch title="IPv6" detail="允许 mihomo 解析与建立 IPv6 连接" checked={network.ipv6} onChange={ipv6 => setNetwork(v => ({ ...v, ipv6 }))}/>
          <SettingSwitch title="TUN 主机路由" detail="让服务器流量进入 mihomo 虚拟网卡" checked={network.tun} onChange={tun => setNetwork(v => ({ ...v, tun }))}/>
        </Stack>
      </CardContent></Card>

      <Card><CardContent>
        <SectionTitle icon={<DeviceHubRounded />} title="端口设置" detail="填 0 可关闭对应的独立监听端口。"/>
        <Divider sx={{ my: 2 }}/>
        <Box className="port-grid">
          <PortField label="混合端口" value={network.mixedPort} onChange={mixedPort => setNetwork(v => ({ ...v, mixedPort }))}/>
          <PortField label="HTTP 端口" value={network.httpPort} onChange={httpPort => setNetwork(v => ({ ...v, httpPort }))}/>
          <PortField label="SOCKS 端口" value={network.socksPort} onChange={socksPort => setNetwork(v => ({ ...v, socksPort }))}/>
          <PortField label="Redirect 端口" value={network.redirPort} onChange={redirPort => setNetwork(v => ({ ...v, redirPort }))}/>
          <PortField label="TProxy 端口" value={network.tproxyPort} onChange={tproxyPort => setNetwork(v => ({ ...v, tproxyPort }))}/>
        </Box>
      </CardContent></Card>

      <Card><CardContent>
        <SectionTitle icon={<DnsRounded />} title="DNS 覆写" detail="由 mihomo 接管 DNS，并选择 Fake-IP 或 Redir-Host。"/>
        <Divider sx={{ my: 2 }}/>
        <Stack spacing={2.5}>
          <SettingSwitch title="启用 DNS 覆写" detail="关闭后使用配置或系统原有的 DNS 路径" checked={network.dnsEnabled} onChange={dnsEnabled => setNetwork(v => ({ ...v, dnsEnabled }))}/>
          <SettingSwitch title="DNS IPv6" detail="允许 DNS 返回 AAAA 记录" checked={network.dnsIPv6} onChange={dnsIPv6 => setNetwork(v => ({ ...v, dnsIPv6 }))}/>
          <TextField label="DNS 监听地址" value={network.dnsListen} onChange={e => setNetwork(v => ({ ...v, dnsListen: e.target.value }))}/>
          <FormControl fullWidth><InputLabel>增强模式</InputLabel><Select value={network.dnsMode} label="增强模式" onChange={e => setNetwork(v => ({ ...v, dnsMode: e.target.value }))}><MenuItem value="fake-ip">Fake-IP</MenuItem><MenuItem value="redir-host">Redir-Host</MenuItem><MenuItem value="normal">Normal</MenuItem></Select></FormControl>
        </Stack>
      </CardContent></Card>

      <Card><CardContent>
        <SectionTitle icon={<MemoryRounded />} title="Clash 内核" detail="只从 MetaCubeX 官方稳定版下载并校验 GitHub SHA-256。"/>
        <Divider sx={{ my: 2 }}/>
        <Stack spacing={2}>
          <FormControl fullWidth><InputLabel>当前内核</InputLabel><Select value={cores.data?.selected || status?.helper?.selectedCore || 'bundled'} label="当前内核" onChange={e => selectCore.mutate(String(e.target.value))} disabled={!cores.data?.cores.length}>{(cores.data?.cores || []).map(core => <MenuItem key={core.id} value={core.id}>{core.bundled ? '内置 · ' : ''}{core.id} · {shortCoreVersion(core.version)}</MenuItem>)}</Select></FormControl>
          <InfoRow label="运行版本" value={status?.core?.version || '—'}/>
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
            <Button variant="outlined" startIcon={<CloudDownloadRounded />} onClick={() => updateCore.mutate()} disabled={updateCore.isPending || !status?.helperOnline}>{updateCore.isPending ? '正在下载并校验…' : '更新到官方稳定版'}</Button>
            <Button variant="outlined" startIcon={<RefreshRounded />} onClick={() => updateGeoData.mutate()} disabled={updateGeoData.isPending || !status?.coreOnline}>{updateGeoData.isPending ? '正在更新 GeoData…' : '更新 GeoData'}</Button>
          </Stack>
          <Alert severity="info" variant="outlined">内置内核始终保留，可随时切回；下载失败不会替换当前运行内核。</Alert>
        </Stack>
      </CardContent></Card>

      <Card><CardContent>
        <SectionTitle icon={<EditRounded />} title="当前配置" detail="查看 Verge 高级设置中的最终运行 YAML。"/>
        <Divider sx={{ my: 2 }}/>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>保存前会用当前选中的 mihomo 内核验证；失败时继续使用原配置。</Typography>
        <Button variant="outlined" startIcon={<EditRounded />} onClick={openCurrentConfig} disabled={!status?.coreOnline}>打开当前配置</Button>
      </CardContent></Card>

      <Card><CardContent>
        <SectionTitle icon={<LockResetRounded />} title="管理员密码" detail="修改后当前会话保持有效。"/>
        <Divider sx={{ my: 2 }}/>
        <Stack spacing={2}><TextField label="当前密码" type="password" value={passwords.current} onChange={e => setPasswords(v => ({ ...v, current: e.target.value }))}/><TextField label="新密码" type="password" helperText="至少 12 个字符" value={passwords.password} onChange={e => setPasswords(v => ({ ...v, password: e.target.value }))}/>{changePassword.error && <Alert severity="error">{(changePassword.error as Error).message}</Alert>}<Button variant="outlined" onClick={() => changePassword.mutate()} disabled={!passwords.current || passwords.password.length < 12}>更新密码</Button></Stack>
      </CardContent></Card>

      <Card><CardContent>
        <SectionTitle icon={<PowerSettingsNewRounded />} title="服务信息" detail="由 systemd 管理的双服务状态。"/>
        <Divider sx={{ my: 2 }}/><InfoRow label="Web 版本" value={status?.appVersion || '—'}/><InfoRow label="监听地址" value={status?.webListen || '—'}/><InfoRow label="内核版本" value={status?.core?.version || '—'}/><InfoRow label="Helper" value={status?.helperOnline ? 'Connected' : 'Offline'}/><InfoRow label="PID" value={String(status?.helper?.pid || '—')}/>
      </CardContent></Card>

      <Card><CardContent>
        <SectionTitle icon={<ShieldRounded />} title="安全边界" detail="Controller 只通过本机 Unix Socket 提供。"/>
        <Divider sx={{ my: 2 }}/><Alert severity="info" variant="outlined">公网部署请在前方配置 HTTPS 反向代理。不要直接暴露 mihomo Controller。</Alert><Stack direction="row" spacing={1} sx={{ mt: 2 }}><Chip label="SameSite session"/><Chip label="Origin protected"/><Chip label="SSRF guarded"/></Stack>
      </CardContent></Card>
    </Box>
    {maintenanceError && <Alert severity="error">{(maintenanceError as Error).message}</Alert>}
    <Button variant="contained" size="large" startIcon={<TuneRounded />} onClick={() => saveCore.mutate()} disabled={!status?.coreOnline || saveCore.isPending}>保存 Clash 设置</Button>
    <CurrentConfigDialog content={currentConfig} setContent={setCurrentConfig} onSave={saveCurrentConfig}/>
    <Snackbar open={!!notice} autoHideDuration={3500} onClose={() => setNotice('')} message={notice}/>
  </Stack>
}

type CoreInfo = { id: string; version: string; selected: boolean; bundled: boolean }
type CoreList = { cores: CoreInfo[]; selected: string }
type NetworkSettings = {
  mode: string; allowLan: boolean; bindAddress: string; ipv6: boolean; tun: boolean
  mixedPort: string; httpPort: string; socksPort: string; redirPort: string; tproxyPort: string
  dnsEnabled: boolean; dnsIPv6: boolean; dnsListen: string; dnsMode: string
}

function settingsFromConfig(config: Record<string, any>): NetworkSettings {
  const port = (value: unknown, fallback = 0) => String(Number.isFinite(Number(value)) ? Number(value) : fallback)
  return {
    mode: String(config.mode || 'rule').toLowerCase(), allowLan: Boolean(config['allow-lan']), bindAddress: String(config['bind-address'] || '*'), ipv6: Boolean(config.ipv6), tun: Boolean(config.tun?.enable),
    mixedPort: port(config['mixed-port'], 7890), httpPort: port(config.port), socksPort: port(config['socks-port']), redirPort: port(config['redir-port']), tproxyPort: port(config['tproxy-port']),
    dnsEnabled: Boolean(config.dns?.enable), dnsIPv6: Boolean(config.dns?.ipv6), dnsListen: String(config.dns?.listen || '0.0.0.0:1053'), dnsMode: String(config.dns?.['enhanced-mode'] || 'fake-ip'),
  }
}

function parsePort(value: string) { const port = Number(value); return Number.isInteger(port) && port >= 0 && port <= 65535 ? port : 0 }
function shortCoreVersion(value: string) { return value.replace(/^Mihomo Meta\s*/i, '').split(/\s+/).slice(0, 2).join(' ') || 'unknown' }
function SettingSwitch({ title, detail, checked, onChange }: { title: string; detail: string; checked: boolean; onChange: (checked: boolean) => void }) { return <Stack direction="row" justifyContent="space-between" alignItems="center" spacing={2}><Box><Typography>{title}</Typography><Typography variant="caption" color="text.secondary">{detail}</Typography></Box><Switch checked={checked} inputProps={{ 'aria-label': title }} onChange={e => onChange(e.target.checked)}/></Stack> }
function PortField({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) { return <TextField label={label} type="number" value={value} inputProps={{ min: 0, max: 65535 }} onChange={e => onChange(e.target.value)}/> }

function CurrentConfigDialog({ content, setContent, onSave }: { content: string | null; setContent: (content: string | null) => void; onSave: () => void }) {
  return <Dialog open={content != null} onClose={() => setContent(null)} fullScreen>
    <DialogTitle><Stack direction="row" alignItems="center" spacing={1}><EditRounded/><span>当前运行配置</span><Chip size="small" label="YAML"/></Stack></DialogTitle>
    <DialogContent sx={{ p: 0 }}>{content != null && <Editor height="calc(100vh - 132px)" defaultLanguage="yaml" theme="vs-dark" value={content} onChange={value => setContent(value || '')} options={{ minimap: { enabled: false }, fontSize: 14, lineHeight: 22, wordWrap: 'on', padding: { top: 18 } }}/>}</DialogContent>
    <DialogActions><Button onClick={() => setContent(null)}>取消</Button><Button variant="contained" onClick={onSave}>验证并应用</Button></DialogActions>
  </Dialog>
}

function ProfileDialog({ open, onClose, onSaved }: { open: boolean; onClose: () => void; onSaved: () => void }) {
  const [value, setValue] = useState({ name: '', url: '', content: '', source: 'remote' }); const [error, setError] = useState('')
  const save = async () => { try { await api('/profiles/', json('POST', value)); setValue({ name: '', url: '', content: '', source: 'remote' }); onSaved() } catch (e) { setError((e as Error).message) } }
  const chooseFile = async (file?: File) => { if (!file) return; if (file.size > 20 * 1024 * 1024) { setError('配置文件不能超过 20 MiB'); return }; const content = await file.text(); setValue(v => ({ ...v, name: v.name || file.name.replace(/\.ya?ml$/i, ''), content, url: '', source: 'local' })) }
  return <Dialog open={open} onClose={onClose} fullWidth maxWidth="md"><DialogTitle>添加配置</DialogTitle><DialogContent><Stack spacing={2} sx={{ mt: 1 }}><TextField label="名称" value={value.name} onChange={e => setValue(v => ({ ...v, name: e.target.value }))}/><TextField label="订阅 URL（与 YAML 二选一）" value={value.url} onChange={e => setValue(v => ({ ...v, url: e.target.value, source: 'remote' }))}/><Divider>或导入本地 YAML</Divider><Button component="label" variant="outlined" startIcon={<UploadFileRounded />}>选择 YAML 文件<input hidden type="file" accept=".yaml,.yml,text/yaml" onChange={e => chooseFile(e.target.files?.[0])}/></Button><TextField multiline minRows={10} label="mihomo YAML" value={value.content} onChange={e => setValue(v => ({ ...v, content: e.target.value, url: '', source: 'local' }))} InputProps={{ sx: { fontFamily: 'ui-monospace, monospace', fontSize: 13 } }}/>{error && <Alert severity="error">{error}</Alert>}</Stack></DialogContent><DialogActions><Button onClick={onClose}>取消</Button><Button variant="contained" onClick={save} disabled={!value.name || (!value.url && !value.content)}>保存配置</Button></DialogActions></Dialog>
}

function EditorDialog({ profile, onClose, onSaved }: { profile: Profile | null; onClose: () => void; onSaved: () => void }) {
  const [content, setContent] = useState(''); const [error, setError] = useState('')
  useEffect(() => setContent(profile?.content || ''), [profile])
  const save = async () => { if (!profile) return; try { await api(`/profiles/${profile.id}`, json('PUT', { name: profile.name, url: profile.url || '', content })); onSaved() } catch (e) { setError((e as Error).message) } }
  return <Dialog open={!!profile} onClose={onClose} fullScreen><DialogTitle><Stack direction="row" alignItems="center" spacing={1}><EditRounded/><span>{profile?.name}</span><Chip size="small" label="YAML"/></Stack></DialogTitle><DialogContent sx={{ p: 0 }}><Editor height="calc(100vh - 132px)" defaultLanguage="yaml" theme="vs-dark" value={content} onChange={v => setContent(v || '')} options={{ minimap: { enabled: false }, fontSize: 14, lineHeight: 22, wordWrap: 'on', padding: { top: 18 } }}/>{error && <Alert severity="error" sx={{ position: 'absolute', bottom: 70, left: 20 }}>{error}</Alert>}</DialogContent><DialogActions><Button onClick={onClose}>取消</Button><Button variant="contained" onClick={save}>验证并保存</Button></DialogActions></Dialog>
}

function PageTools({ title, query, setQuery, action }: { title: string; query?: string; setQuery?: (s: string) => void; action?: React.ReactNode }) { return <Stack direction={{ xs: 'column', sm: 'row' }} gap={1.5} alignItems={{ sm: 'center' }}><Typography variant="h5" flex={1}>{title}</Typography>{setQuery && <TextField size="small" placeholder="筛选…" value={query} onChange={e => setQuery(e.target.value)} InputProps={{ startAdornment: <InputAdornment position="start"><SearchRounded fontSize="small" /></InputAdornment> }} />}{action}</Stack> }
function StatCard({ icon, label, value, foot }: { icon: React.ReactNode; label: string; value: string; foot: string }) { return <Card className="stat-card"><CardContent><Box className="stat-icon">{icon}</Box><Typography variant="caption" color="text.secondary">{label.toUpperCase()}</Typography><Typography variant="h6" className="mono">{value}</Typography><Typography variant="caption" color="text.secondary">{foot}</Typography></CardContent></Card> }
function SectionTitle({ icon, title, detail }: { icon: React.ReactNode; title: string; detail: string }) { return <Stack direction="row" spacing={1.5}><Box className="section-icon">{icon}</Box><Box><Typography variant="h6">{title}</Typography><Typography variant="body2" color="text.secondary">{detail}</Typography></Box></Stack> }
function InfoRow({ label, value }: { label: string; value: string }) { return <Stack direction="row" justifyContent="space-between" sx={{ py: 1 }}><Typography color="text.secondary">{label}</Typography><Typography className="mono">{value}</Typography></Stack> }
function Metric({ label, value, color }: { label: string; value: string; color: string }) { return <Box><Typography variant="caption" color="text.secondary">{label}</Typography><Typography variant="h5" className="mono" sx={{ color }}>{value}</Typography></Box> }
function EmptyGuide({ title, detail, action }: { title: string; detail: string; action?: React.ReactNode }) { return <Card className="empty"><CardContent><Box className="empty-icon"><DeviceHubRounded /></Box><Typography variant="h6">{title}</Typography><Typography color="text.secondary" sx={{ mb: action ? 2 : 0 }}>{detail}</Typography>{action}</CardContent></Card> }
function OfflineState() { return <EmptyGuide title="mihomo 内核离线" detail="启用一个有效配置或在首页启动内核后，此页面会自动连接。" /> }
function CenteredProgress() { return <Box className="center"><CircularProgress size={28}/></Box> }
function FatalState({ message, onRetry }: { message: string; onRetry: () => void }) { return <Box className="center"><Alert severity="error" action={<Button onClick={onRetry}>重试</Button>}>{message}</Alert></Box> }

function TrafficChart({ points }: { points: TrafficPoint[] }) { const up = points.map(p => p.up), down = points.map(p => p.down); return <svg className="traffic-chart" viewBox="0 0 500 100" preserveAspectRatio="none"><polyline className="line down" points={spark(down, 500, 100)}/><polyline className="line up" points={spark(up, 500, 100)}/></svg> }
function TelemetryRail({ points, online }: { points: TrafficPoint[]; online: boolean }) { const p = points.at(-1) || { up: 0, down: 0, upTotal: 0, downTotal: 0 }; return <Box className="telemetry"><TrafficChart points={points}/><Stack direction="row" justifyContent="space-between"><Box><Typography variant="caption">↑ UP</Typography><Typography className="mono up-value">{rate(p.up)}</Typography></Box><Box textAlign="right"><Typography variant="caption">↓ DOWN</Typography><Typography className="mono down-value">{rate(p.down)}</Typography></Box></Stack><Divider sx={{ my: 1.2 }}/><Stack direction="row" justifyContent="space-between"><Typography variant="caption" color="text.secondary">SESSION</Typography><Typography variant="caption" className="mono">{bytes(p.upTotal + p.downTotal)}</Typography></Stack><Box className={online ? 'rail-online on' : 'rail-online'}>{online ? 'LIVE LINK' : 'OFFLINE'}</Box></Box> }
function spark(values: number[], width: number, height: number) { const data = values.length > 1 ? values : [0, 0]; const max = Math.max(...data, 1); return data.map((v, i) => `${(i / (data.length - 1)) * width},${height - (v / max) * (height - 10) - 5}`).join(' ') }
function rate(v: number) { return `${bytes(v)}/s` }
function bytes(v: number) { if (!Number.isFinite(v) || v <= 0) return '0 B'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; const i = Math.min(Math.floor(Math.log(v) / Math.log(1024)), units.length - 1); return `${(v / 1024 ** i).toFixed(i > 1 ? 1 : 0)} ${units[i]}` }
function displayTime(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : date.toLocaleString() }

function useLiveStream(topic: string, enabled: boolean, onMessage: (data: any) => void) {
  const callback = useRef(onMessage)
  callback.current = onMessage
  useEffect(() => {
    if (!enabled) return
    let socket: WebSocket | undefined; let timer = 0; let closed = false
    const connect = () => { socket = new WebSocket(wsURL(topic)); socket.onmessage = event => { try { callback.current(JSON.parse(event.data)) } catch { /* ignore malformed frame */ } }; socket.onclose = () => { if (!closed) timer = window.setTimeout(connect, 1800) } }
    connect(); return () => { closed = true; window.clearTimeout(timer); socket?.close() }
  }, [topic, enabled])
}
