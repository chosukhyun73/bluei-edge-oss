import { useEffect, useState } from 'react';
import { dateLocale } from '../../lib/format';
import { useLanguage } from '../../lib/language-core';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { FeedCycles } from '../../lib/api';
import type { FeedCycleReport } from '../../lib/types';
import { effectivePulseVerdicts } from '../../lib/pulse-verdict';

// 사이클 완료 자동 보고 카드 — backend feed.cycle.report 폴링 (30초).
// 매 급이 사이클 종료 시 backend 가 사이클 사실 + 비전 결과(LRCN·요동곡선)를 종합해
// 적재한 보고를 최신순으로 노출한다. Claude 세션 없이 365일 동작하는 무인 보고 경로.

// 사이클 배출 패턴 일치도 경고 임계 — backend patternMatchWeak(pattern_match.go)과 동기 유지.
const PATTERN_MATCH_WEAK = 0.0;

// 종료 사유 → i18n 키 + 톤. 정상(목표/상한 도달)=green, 운영자/장비=amber, 안전=red.
const REASON_META: Record<string, { key: string; tone: 'ok' | 'warn' | 'bad' }> = {
  satiation_or_target: { key: 'feedingReport.reasonTarget', tone: 'ok' },
  max_pulses: { key: 'feedingReport.reasonMaxPulses', tone: 'ok' },
  max_duration: { key: 'feedingReport.reasonMaxDuration', tone: 'warn' },
  operator_stop: { key: 'feedingReport.reasonOperatorStop', tone: 'warn' },
  silo_threshold: { key: 'feedingReport.reasonSilo', tone: 'warn' },
  safety_block: { key: 'feedingReport.reasonSafetyBlock', tone: 'bad' },
  aborted_on_startup: { key: 'feedingReport.reasonAborted', tone: 'warn' },
};

const TONE_CLS: Record<'ok' | 'warn' | 'bad', string> = {
  ok: 'bg-green-500/15 text-green-300 border-green-500/30',
  warn: 'bg-amber-500/15 text-amber-300 border-amber-500/30',
  bad: 'bg-red-500/15 text-red-300 border-red-500/30',
};

function ReasonBadge({ reason }: { reason?: string | null }) {
  const { tr } = useLanguage();
  const meta = (reason && REASON_META[reason]) || null;
  const label = meta ? tr(meta.key) : (reason ?? '—');
  const cls = TONE_CLS[meta?.tone ?? 'warn'];
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] font-medium ${cls}`}>
      {label}
    </span>
  );
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString(dateLocale(), { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

export function FeedingReportCard() {
  const { tr } = useLanguage();
  const [reports, setReports] = useState<FeedCycleReport[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const fetchReports = () => {
      FeedCycles.reports(20)
        .then(r => {
          if (cancelled) return;
          setReports(r.items ?? []);
          setLoaded(true);
        })
        .catch(() => { if (!cancelled) setLoaded(true); /* 이전 데이터 유지 */ });
    };
    fetchReports();
    const id = setInterval(fetchReports, 30_000);
    return () => { cancelled = true; clearInterval(id); };
  }, []);

  return (
    <Card>
      <CardHeader className="border-b border-gray-700/30">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-white">
          <span>📋</span>
          <span>{tr('feedingReport.title')}</span>
          <span className="text-xs text-gray-500 font-normal">{tr('feedingReport.subtitle')}</span>
        </CardTitle>
      </CardHeader>
      <CardContent className="pt-3">
        {!loaded ? (
          <p className="text-xs text-gray-500 py-2">…</p>
        ) : reports.length === 0 ? (
          <p className="text-xs text-gray-500 py-2">{tr('feedingReport.empty')}</p>
        ) : (
          <ul className="divide-y divide-gray-800 max-h-72 overflow-y-auto">
            {reports.map(rep => {
              const dispensedG =
                rep.actual_total_amount_g && rep.actual_total_amount_g > 0
                  ? rep.actual_total_amount_g
                  : rep.total_amount_g;
              const lrcnScore = rep.lrcn?.mean_score;
              const risePct = rep.agitation
                ? Math.round(rep.agitation.max_peak_rise_frac * 100)
                : null;
              return (
                <li key={rep.report_id} className="py-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                  <span className="font-mono text-gray-400 shrink-0">{fmtTime(rep.completed_at)}</span>
                  <span className="font-mono text-white shrink-0">{rep.tank_id}</span>
                  <span className="text-gray-500 shrink-0">
                    {rep.priority === 'manual_override' ? `✋ ${tr('feedingReport.manual')}` : `🤖 AI`}
                  </span>
                  <ReasonBadge reason={rep.termination_reason} />
                  <span className="font-mono text-gray-200">
                    {dispensedG.toFixed(1)}g
                    {rep.target_amount_g && rep.target_amount_g > 0 && (
                      <span className="text-gray-500"> / {rep.target_amount_g.toFixed(0)}g</span>
                    )}
                  </span>
                  <span className="text-gray-400">
                    {tr('feedingReport.pulses', { n: rep.pulses_executed })}
                    {' · '}
                    {Math.round(rep.duration_s / 60)}{tr('feedingReport.minUnit')}
                  </span>
                  {lrcnScore != null && (
                    <span className="text-cyan-300 font-mono" title={tr('feedingReport.lrcnHint')}>
                      LRCN {lrcnScore.toFixed(2)}
                    </span>
                  )}
                  {risePct != null && (
                    <span className="text-blue-300 font-mono" title={tr('feedingReport.agitationHint')}>
                      {tr('feedingReport.agitation')} +{risePct}%
                    </span>
                  )}
                  {rep.agitation?.startle_suspect && (
                    <span
                      className="inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] bg-purple-500/15 text-purple-300 border-purple-500/30"
                      title={tr('feedingReport.startleHint')}
                    >
                      {tr('feedingReport.startle')}
                    </span>
                  )}
                  {/* 섀도 중단 — v1(배출 확인 후 반응 판정) 우선. v1 은 무배출을
                      식욕 판단에서 빼므로 급이기 고장을 만복으로 읽지 않는다.
                      v1 이 없는 옛 보고만 v0 로 폴백 (2026-08-03). */}
                  {rep.shadow_judge?.v1
                    ? rep.shadow_judge.v1.stop_at_pulse > 0 && (
                      <span
                        className="inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] bg-amber-500/15 text-amber-300 border-amber-500/30"
                        title={tr('feedingReport.shadowStopHint', {
                          values: rep.shadow_judge.v1.values.map(v => v.toFixed(2)).join(' '),
                          min: rep.shadow_judge.v1.discharge_min.toFixed(2),
                          low: rep.shadow_judge.v1.low_reaction.toFixed(2),
                        })}
                      >
                        {tr('feedingReport.shadowJudgeStop', { n: rep.shadow_judge.v1.stop_at_pulse })}
                      </span>
                    )
                    : rep.shadow_judge && rep.shadow_judge.stop_at_pulse > 0 && (
                      <span
                        className="inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] bg-amber-500/15 text-amber-300 border-amber-500/30"
                        title={tr('feedingReport.shadowJudgeHint', {
                          priors: rep.shadow_judge.priors.map(p => p.toFixed(2)).join(' '),
                          track: rep.shadow_judge.track,
                        })}
                      >
                        {tr('feedingReport.shadowJudgeStop', { n: rep.shadow_judge.stop_at_pulse })}
                      </span>
                    )}
                  {rep.shadow_judge?.v1?.feeder_fault && (
                    <span
                      className="inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] bg-red-500/15 text-red-300 border-red-500/30"
                      title={tr('feedingReport.feederFaultHint', {
                        n: rep.shadow_judge.v1.no_discharge,
                        total: rep.shadow_judge.v1.values.length,
                      })}
                    >
                      {tr('feedingReport.feederFault', { n: rep.shadow_judge.v1.no_discharge })}
                    </span>
                  )}
                  {/* 펄스별 실배출 — 무배출 의심 펄스가 있으면 명확히 경고 (2026-07-08 운영자 지시).
                      라이브(gap끝) verdict 우선 병합 (2026-07-10 운영자 승인) — 보고 채널은
                      보상 피크가 σ 를 오염시켜 과비관 (7-09 육안 대조 실측). */}
                  {(() => {
                    if (!rep.discharge_check) return null;
                    // 게이트 보류 펄스는 미급이로 세지 않는다 (lib/pulse-verdict.ts)
                    const merged = effectivePulseVerdicts(rep);
                    const suspected = merged.filter(v => v === 'no').length;
                    if (suspected === 0) return null;
                    return (
                      <span
                        className="inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] bg-red-500/15 text-red-300 border-red-500/40"
                        title={tr('feedingReport.dischargeHint', {
                          marks: merged
                            .map((v, i) => `p${i + 1}:${v === 'yes' ? 'O' : v === 'unclear' ? '~' : v === 'no' ? 'X' : '?'}`)
                            .join(' '),
                        })}
                      >
                        {tr('feedingReport.dischargeSuspect', { n: suspected, total: merged.length })}
                      </span>
                    );
                  })()}
                  {/* 사이클 통째 배출 패턴 (2026-07-29 운영자 지시) — 펄스 열과 요동 봉우리
                      열의 일치도. 펄스별 판정이 포화로 무너져도 남는 신호라 급이기 고장을
                      사이클 단위로 잡는다. 급이 결정에는 개입하지 않는 관측 지표. */}
                  {typeof rep.discharge_check?.pattern_match === 'number' && (() => {
                    const m = rep.discharge_check.pattern_match;
                    const weak = m < PATTERN_MATCH_WEAK;
                    return (
                      <span
                        className={`inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] ${
                          weak
                            ? 'bg-red-500/15 text-red-300 border-red-500/40'
                            : 'bg-gray-500/10 text-gray-400 border-gray-600/40'
                        }`}
                        title={tr('feedingReport.patternHint')}
                      >
                        {tr(weak ? 'feedingReport.patternWeak' : 'feedingReport.patternMatch', {
                          v: m.toFixed(2),
                        })}
                      </span>
                    );
                  })()}
                  {/* gap끝 복귀도 — 마지막 펄스가 미복귀면 "종료 시 섭이 지속" (증량 근거 후보) */}
                  {rep.shadow_judge?.gap_end_recovery &&
                    rep.shadow_judge.gap_end_recovery.length > 0 &&
                    rep.shadow_judge.gap_end_recovery[rep.shadow_judge.gap_end_recovery.length - 1] === false && (
                    <span
                      className="inline-flex items-center px-1.5 py-0.5 rounded border text-[11px] bg-cyan-500/15 text-cyan-300 border-cyan-500/30"
                      title={tr('feedingReport.gapEndHint', {
                        ratios: (rep.shadow_judge.gap_end_ratio ?? []).map(r => r.toFixed(2)).join(' '),
                      })}
                    >
                      {tr('feedingReport.gapEndNotRecovered', {
                        n: rep.shadow_judge.gap_end_recovery.filter(v => !v).length,
                        total: rep.shadow_judge.gap_end_recovery.length,
                      })}
                    </span>
                  )}
                  {rep.clip_count === 0 && (
                    <span className="text-gray-600">{tr('feedingReport.noClip')}</span>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
