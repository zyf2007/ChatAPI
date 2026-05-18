import { useEffect, useState } from 'react'
import dayjs, { type Dayjs } from 'dayjs'
import { Button, Card, DatePicker, Typography } from 'antd'

import { requestJson } from '../../lib/api'
import type { StatisticsSummary } from '../../types/chat'

const { RangePicker } = DatePicker

const STATISTICS_PRESETS = [
  { key: '3h', label: '最近3小时', hours: 3 },
  { key: '6h', label: '最近6小时', hours: 6 },
  { key: '12h', label: '最近12小时', hours: 12 },
  { key: '24h', label: '最近24小时', hours: 24 },
  { key: '48h', label: '最近48小时', hours: 48 },
  { key: 'all', label: '所有时间', hours: 0 },
] as const

type StatisticsPresetKey = (typeof STATISTICS_PRESETS)[number]['key'] | 'custom'

function getPresetRange(preset: StatisticsPresetKey): [Dayjs | null, Dayjs | null] {
  if (preset === 'all') {
    return [null, null]
  }
  const presetHours = STATISTICS_PRESETS.find((item) => item.key === preset)?.hours ?? 24
  return [dayjs().subtract(presetHours, 'hour'), dayjs()]
}

function formatNumber(value: number, maximumFractionDigits = 0): string {
  return new Intl.NumberFormat('zh-CN', {
    maximumFractionDigits,
  }).format(value)
}

function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return '0 秒'
  }
  if (seconds < 60) {
    return `${seconds.toFixed(seconds < 10 ? 1 : 0)} 秒`
  }
  const totalMinutes = seconds / 60
  if (totalMinutes < 60) {
    return `${totalMinutes.toFixed(totalMinutes < 10 ? 1 : 0)} 分钟`
  }
  const hours = Math.floor(totalMinutes / 60)
  const minutes = Math.round(totalMinutes % 60)
  return `${hours} 小时 ${minutes} 分钟`
}

type StatisticsPanelProps = {
  open: boolean
}

export function StatisticsPanel({ open }: StatisticsPanelProps) {
  const [preset, setPreset] = useState<StatisticsPresetKey>('24h')
  const [range, setRange] = useState<[Dayjs | null, Dayjs | null]>(() => getPresetRange('24h'))
  const [summary, setSummary] = useState<StatisticsSummary | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setPreset('24h')
    setRange(getPresetRange('24h'))
  }, [open])

  useEffect(() => {
    if (!open) return

    let active = true

    async function loadStatistics() {
      const [start, end] = range
      const params = new URLSearchParams()
      if (start) {
        params.set('start', start.toISOString())
      }
      if (end) {
        params.set('end', end.toISOString())
      }
      setLoading(true)
      setError('')
      try {
        const data = await requestJson<{ ok: boolean; summary: StatisticsSummary }>(
          `/api/statistics/summary${params.toString() ? `?${params.toString()}` : ''}`,
        )
        if (!active) return
        setSummary(data.summary)
      } catch (error_) {
        if (!active) return
        setError(error_ instanceof Error ? error_.message : '统计加载失败')
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void loadStatistics()

    return () => {
      active = false
    }
  }, [open, range])

  function applyPreset(nextPreset: StatisticsPresetKey) {
    setPreset(nextPreset)
    setRange(getPresetRange(nextPreset))
  }

  const fallbackSummary = {
    total_requests: 0,
    average_request_time_seconds: 0,
    average_tpm: 0,
    total_tokens: 0,
    input_tokens: 0,
    output_tokens: 0,
  }
  const current = summary ?? fallbackSummary

  return (
    <div className="statistics-panel">
      <div className="statistics-toolbar">
        <div className="statistics-toolbar-copy">
          <Typography.Text className="eyebrow">统计面板</Typography.Text>
          <Typography.Title level={5} className="statistics-title">
            统计范围内的请求与 token 消耗
          </Typography.Title>
        </div>
        <div className="statistics-range-picker">
          <RangePicker
            showTime
            value={range}
            onChange={(dates) => {
              const nextRange: [Dayjs | null, Dayjs | null] = [
                dates?.[0] ?? null,
                dates?.[1] ?? null,
              ]
              setPreset('custom')
              setRange(nextRange)
            }}
            allowClear
          />
        </div>
      </div>

      <div className="statistics-presets">
        {STATISTICS_PRESETS.map((item) => (
          <Button
            key={item.key}
            type={preset === item.key ? 'primary' : 'default'}
            onClick={() => applyPreset(item.key)}
          >
            {item.label}
          </Button>
        ))}
      </div>

      <div className="statistics-grid">
        <Card className="statistics-card" loading={loading}>
          <Typography.Text className="statistics-card-label">总请求数</Typography.Text>
          <Typography.Title level={2} className="statistics-card-value">
            {formatNumber(current.total_requests)}
          </Typography.Title>
        </Card>
        <Card className="statistics-card" loading={loading}>
          <Typography.Text className="statistics-card-label">平均请求时间</Typography.Text>
          <Typography.Title level={2} className="statistics-card-value">
            {formatDuration(current.average_request_time_seconds)}
          </Typography.Title>
        </Card>
        <Card className="statistics-card" loading={loading}>
          <Typography.Text className="statistics-card-label">平均 TPM</Typography.Text>
          <Typography.Title level={2} className="statistics-card-value">
            {formatNumber(current.average_tpm, 1)}
          </Typography.Title>
        </Card>
        <Card className="statistics-card statistics-card-tokens" loading={loading}>
          <Typography.Text className="statistics-card-label">总 token 数</Typography.Text>
          <Typography.Title level={2} className="statistics-card-value">
            {formatNumber(current.total_tokens)}
          </Typography.Title>
          <div className="statistics-token-split">
            <span>输入 {formatNumber(current.input_tokens)}</span>
            <span>输出 {formatNumber(current.output_tokens)}</span>
          </div>
        </Card>
      </div>

      {error ? <div className="statistics-error">{error}</div> : null}
    </div>
  )
}
