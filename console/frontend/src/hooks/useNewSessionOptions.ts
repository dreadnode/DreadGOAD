import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type ConfigListing, type LabSummary } from '../api'
import { NEW_ENV, OTHER_PATH } from '../newSessionModel'

interface NewSessionOptions {
  listing: ConfigListing | null
  envs: string[]
  envErr: string
  configOk: boolean
  loading: boolean
  envChoice: string
  setEnvChoice: (value: string) => void
  loadedProvider: string
  loadedProviders: Record<string, string>
  loadedRegions: Record<string, string | null>
  loadedPath: string
  labList: LabSummary[]
  source: string
  setSource: (value: string) => void
  loadEnvs: (path: string) => Promise<void>
}

/** Load the config-dependent choices while ensuring only the newest request wins. */
export function useNewSessionOptions(
  choice: string,
  customPath: string,
  creatingConfig: boolean,
): NewSessionOptions {
  const [listing, setListing] = useState<ConfigListing | null>(null)
  const [envs, setEnvs] = useState<string[]>([])
  const [envErr, setEnvErr] = useState('')
  const [configOk, setConfigOk] = useState(false)
  const [loading, setLoading] = useState(false)
  const [envChoice, setEnvChoice] = useState('')
  const [loadedProvider, setLoadedProvider] = useState('')
  const [loadedProviders, setLoadedProviders] = useState<Record<string, string>>({})
  const [loadedRegions, setLoadedRegions] = useState<Record<string, string | null>>({})
  const [loadedPath, setLoadedPath] = useState('')
  const [labList, setLabList] = useState<LabSummary[]>([])
  const [source, setSource] = useState('ad/GOAD')
  const environmentRequest = useRef(0)

  useEffect(() => () => {
    // Invalidate a blur-triggered request too; unlike effect-owned requests it
    // has no individual cleanup callback when the modal unmounts.
    ++environmentRequest.current
  }, [])

  useEffect(() => {
    let cancelled = false
    api.configs()
      .then(result => { if (!cancelled) setListing(result) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [])

  const clearLoadedConfig = useCallback(() => {
    setEnvs([])
    setEnvChoice('')
    setConfigOk(false)
    setLoadedProvider('')
    setLoadedProviders({})
    setLoadedRegions({})
    setLoadedPath('')
  }, [])

  const loadEnvs = useCallback(async (path: string) => {
    const request = ++environmentRequest.current
    const trimmedPath = path.trim()
    if (!trimmedPath) {
      clearLoadedConfig()
      setLoading(false)
      setEnvErr('config path is required')
      return
    }

    setLoading(true)
    setEnvErr('')
    try {
      const result = await api.environments(trimmedPath)
      if (request !== environmentRequest.current) return
      setEnvs(result.environments)
      setConfigOk(true)
      setLoadedPath(trimmedPath)
      setLoadedProvider(result.provider || '')
      setLoadedProviders(result.env_providers || {})
      setLoadedRegions(result.env_regions || {})
      setEnvChoice(previous => result.environments.includes(previous)
        ? previous
        : (result.environments[0] || NEW_ENV))
    } catch (error) {
      if (request !== environmentRequest.current) return
      clearLoadedConfig()
      setEnvErr(error instanceof Error ? error.message : String(error))
    } finally {
      if (request === environmentRequest.current) setLoading(false)
    }
  }, [clearLoadedConfig])

  useEffect(() => {
    if (creatingConfig) {
      ++environmentRequest.current
      setEnvs([])
      setConfigOk(true)
      setEnvErr('')
      setLoading(false)
      return
    }
    if (choice === OTHER_PATH) {
      ++environmentRequest.current
      clearLoadedConfig()
      setLoading(false)
      setEnvErr(customPath.trim() ? '' : 'enter a path, then click away to load it')
      if (customPath.trim()) void loadEnvs(customPath)
      return
    }
    void loadEnvs(choice)
    // customPath is deliberately excluded: custom paths load on blur, not on
    // every keystroke. It is read only when the picker changes to Other path.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [choice, creatingConfig, clearLoadedConfig, loadEnvs])

  // Labs live below the successfully loaded config's own project root. New
  // configs use this checkout; unsettled custom paths make no request.
  const labsKey: string | null = creatingConfig ? '' : (loadedPath || null)
  useEffect(() => {
    if (labsKey === null) return
    let cancelled = false
    api.labs(labsKey || undefined)
      .then(result => {
        if (cancelled) return
        setLabList(result.labs)
        setSource(previous => result.labs.length && !result.labs.some(lab => lab.dir === previous)
          ? result.labs[0].dir
          : previous)
      })
      .catch(() => { if (!cancelled) setLabList([]) })
    return () => { cancelled = true }
  }, [labsKey])

  return {
    listing,
    envs,
    envErr,
    configOk,
    loading,
    envChoice,
    setEnvChoice,
    loadedProvider,
    loadedProviders,
    loadedRegions,
    loadedPath,
    labList,
    source,
    setSource,
    loadEnvs,
  }
}
