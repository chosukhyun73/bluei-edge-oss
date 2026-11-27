import { useEffect, useMemo, useRef, useState } from 'react';
import { layoutFlow, FLOW_CARD_W, FLOW_CARD_H } from '../../lib/plant';
import type { PlanContent, PlantZone } from '../../lib/plant';
import { useLanguage } from '../../lib/language-core';

// 공정도 — 단계는 카드, 연결은 화살표. 목록에서 고친 내용이 그대로 그려진다(그림에서 직접 고치지는 않는다).
// CCP 는 카드와 연결 위에 표시한다: 단계 CCP(빨강), 구간 CCP(주황, 여러 단계에 같은 표시), 연결 CCP(연결 위).

export type FlowSelection = { kind: 'step' | 'link'; id: string } | null;

export function FlowDiagram({ content, zones, selected, onSelect, current, ccpStatus }: {
  content: PlanContent;
  zones: PlantZone[];
  selected: FlowSelection;
  onSelect: (s: FlowSelection) => void;
  // 생산 화면: lot 이 지금 있는 단계(또는 이동 중인 연결)와 CCP 별 판정.
  current?: { stepId: string; linkId: string };
  ccpStatus?: Record<string, string>;
}) {
  const { tr } = useLanguage();
  const wrapRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(900);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    setWidth(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const steps = content.steps ?? [];
  const links = content.links ?? [];
  const ccps = content.ccps ?? [];
  const hazards = content.hazards ?? [];
  const layout = useMemo(() => layoutFlow(steps, links, width), [steps, links, width]);
  const zoneName = new Map(zones.map(z => [z.zone_id, z.name]));
  const linkOf = new Map(links.map(l => [l.link_id, l]));
  // CCP 표시 색: 생산 화면에서는 판정에 따라, 계획 화면에서는 종류에 따라.
  const badge = (ccpId: string, fallback: string) => {
    const st = ccpStatus?.[ccpId];
    if (!ccpStatus) return fallback;
    if (st === 'deviation' || st === 'outage') return 'bg-red-600 text-white';
    if (st === 'ok') return 'bg-green-600/30 text-green-200';
    if (st === 'needs_input' || st === 'in_progress') return 'bg-amber-500/30 text-amber-200';
    return 'bg-gray-700 text-gray-300';
  };

  if (steps.length === 0) {
    return <div ref={wrapRef} className="py-8 text-center text-sm text-gray-500">{tr('plant.flow.empty')}</div>;
  }

  return (
    <div ref={wrapRef} className="overflow-x-auto">
      <div className="relative" style={{ width: layout.width, height: layout.height }}>
        <svg className="absolute inset-0" width={layout.width} height={layout.height}>
          <defs>
            <marker id="flow-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto">
              <path d="M0,0 L10,5 L0,10 z" fill="#6b7280" />
            </marker>
          </defs>
          {layout.edges.map(e => (
            <path
              key={e.id} d={e.d} fill="none" markerEnd="url(#flow-arrow)"
              stroke={current?.linkId === e.id ? '#38bdf8' : selected?.kind === 'link' && selected.id === e.id ? '#4ade80' : '#6b7280'}
              strokeWidth={current?.linkId === e.id || (selected?.kind === 'link' && selected.id === e.id) ? 2.5 : 1.5}
              strokeDasharray={e.rework ? '5 4' : undefined}
            />
          ))}
        </svg>

        {layout.edges.map(e => {
          const l = linkOf.get(e.id);
          if (!l) return null;
          const linkCCPs = ccps.filter(k => k.scope_kind === 'link' && (k.link_ids ?? []).includes(e.id));
          const wait = l.max_wait_min != null ? tr('plant.flow.waitMax', { min: l.max_wait_min }) : '';
          const moving = current?.linkId === e.id;
          if (linkCCPs.length === 0 && !wait && !moving) return null;
          return (
            <button
              key={e.id} type="button" onClick={() => onSelect({ kind: 'link', id: e.id })}
              className={'absolute -translate-x-1/2 -translate-y-1/2 flex items-center gap-1 px-1.5 py-0.5 rounded bg-gray-950 border text-[10px] text-gray-300 whitespace-nowrap ' + (moving ? 'border-sky-400' : 'border-gray-700')}
              style={{ left: e.labelX, top: e.labelY }}
            >
              {moving && <span className="px-1 rounded bg-sky-500 text-white font-semibold">{tr('proc.flow.moving')}</span>}
              {linkCCPs.map(k => <span key={k.ccp_id} className={'px-1 rounded font-semibold ' + badge(k.ccp_id, 'bg-red-500/20 text-red-300')}>{k.code}</span>)}
              {wait}
            </button>
          );
        })}

        {layout.nodes.map((n, i) => {
          const s = steps[i];
          const stepCCPs = ccps.filter(k => k.scope_kind === 'step' && (k.step_ids ?? []).includes(s.step_id));
          const spanCCPs = ccps.filter(k => k.scope_kind === 'span' && (k.step_ids ?? []).includes(s.step_id));
          const hazardCount = hazards.filter(h => h.step_id === s.step_id).length;
          const isSelected = selected?.kind === 'step' && selected.id === s.step_id;
          const isCurrent = current?.stepId === s.step_id;
          return (
            <button
              key={n.id} type="button" onClick={() => onSelect({ kind: 'step', id: s.step_id })}
              className={
                'absolute flex flex-col justify-between text-left rounded-lg border px-2.5 py-1.5 transition-colors ' +
                (isCurrent ? 'border-sky-400 bg-sky-500/15 ring-2 ring-sky-400/40 '
                  : isSelected ? 'border-green-400 bg-green-500/10 '
                  : stepCCPs.length ? 'border-red-500/60 bg-red-500/5 hover:border-red-400 '
                  : spanCCPs.length ? 'border-amber-500/60 bg-amber-500/5 hover:border-amber-400 '
                  : 'border-gray-700 bg-gray-900 hover:border-gray-500 ')
              }
              style={{ left: n.x, top: n.y, width: FLOW_CARD_W, height: FLOW_CARD_H }}
            >
              <div className="flex items-start gap-1.5">
                <span className="text-[10px] text-gray-500 font-mono mt-0.5">{i + 1}</span>
                <span className="text-sm text-white leading-tight line-clamp-2">{s.name || tr('plant.flow.unnamed')}</span>
              </div>
              <div className="flex items-center gap-1 text-[10px] overflow-hidden whitespace-nowrap">
                {isCurrent && <span className="px-1 rounded bg-sky-500 text-white font-semibold">{tr('proc.flow.here')}</span>}
                {stepCCPs.map(k => <span key={k.ccp_id} className={'px-1 rounded font-semibold ' + badge(k.ccp_id, 'bg-red-500/20 text-red-300')}>{k.code}</span>)}
                {spanCCPs.map(k => <span key={k.ccp_id} className={'px-1 rounded font-semibold ' + badge(k.ccp_id, 'bg-amber-500/20 text-amber-300')}>{k.code}</span>)}
                {hazardCount > 0 && !isCurrent && <span className="text-gray-500">{tr('plant.flow.hazardCount', { n: hazardCount })}</span>}
                <span className="ml-auto text-gray-500 truncate">{zoneName.get(s.zone_id) ?? ''}</span>
              </div>
            </button>
          );
        })}
      </div>

      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-gray-500">
        <span><span className="inline-block w-2.5 h-2.5 rounded-sm bg-red-500/40 mr-1 align-middle" />{tr('plant.flow.legendStep')}</span>
        <span><span className="inline-block w-2.5 h-2.5 rounded-sm bg-amber-500/40 mr-1 align-middle" />{tr('plant.flow.legendSpan')}</span>
        <span>{tr('plant.flow.legendLink')}</span>
      </div>
    </div>
  );
}
