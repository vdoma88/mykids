import type { JSX } from 'react';
import { useState } from 'react';
import { api, type ChildDetail } from '../../api.js';
import { PLATFORMS } from '../../lib/labels.js';
import { ErrorBox, errorText } from '../../ui/Async.js';
import { Icon, type IconName } from '../../ui/Icon.js';
import { Alert, Badge, Card, ConfirmButton, CopyButton, Empty, Field, Tabs } from '../../ui/kit.js';
import { useToast } from '../../ui/Toast.js';
import { deviceHealth } from '../Children.js';

type Platform = 'windows' | 'android' | 'web';
const PLATFORM_ICON: Record<string, IconName> = { windows: 'monitor', android: 'phone', web: 'globe' };

/** Что делать с токеном дальше — у каждой платформы своё. */
function NextSteps({ platform }: { platform: Platform }): JSX.Element {
  const childUrl = `${window.location.origin}/child`;
  if (platform === 'windows') {
    return (
      <ol className="small" style={{ margin: 0, paddingLeft: 18 }}>
        <li>На компьютере ребёнка запустите «MyKids Setup» от имени администратора.</li>
        <li>Впишите адрес сервера <span className="mono">{window.location.origin}</span>, этот токен и имя учётной записи ребёнка.</li>
        <li>Проверьте из учётной записи ребёнка через «MyKids Check».</li>
      </ol>
    );
  }
  if (platform === 'web') {
    return (
      <p className="small">
        Откройте на устройстве ребёнка <span className="mono">{childUrl}</span> и вставьте токен.
        Там он решает задания и меняет кредиты на время.
      </p>
    );
  }
  return <p className="small">Android-агент ещё в разработке: токен пригодится, когда он появится.</p>;
}

export function DevicesTab({ child, readOnly, onChanged }: {
  child: ChildDetail; readOnly: boolean; onChanged: () => void;
}): JSX.Element {
  const [platform, setPlatform] = useState<Platform>('windows');
  const [name, setName] = useState('ПК ребёнка');
  const [issued, setIssued] = useState<{ token: string; platform: Platform } | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function enroll(): Promise<void> {
    setErr(null);
    setBusy(true);
    try {
      const r = await api.enrollDevice(child.id, { platform, name: name.trim() || PLATFORMS[platform]! });
      setIssued({ token: r.token, platform });
      onChanged();
    } catch (e) {
      setIssued(null);
      setErr(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  async function revoke(id: string, deviceName: string): Promise<void> {
    try {
      await api.revokeDevice(id);
      toast.success(`Устройство «${deviceName}» отвязано: его токен больше не действует.`);
      onChanged();
    } catch (e) {
      toast.error(errorText(e));
    }
  }

  return (
    <>
      <Card title="Устройства" desc="Где стоит агент. Молчащий больше суток агент мог быть снят — это повод поговорить.">
        {child.devices.length === 0 ? (
          <Empty icon="monitor" title="Устройств нет">Выдайте токен ниже и впишите его в установщик на компьютере ребёнка.</Empty>
        ) : (
          <div className="list">
            {child.devices.map((d) => {
              const h = deviceHealth(d);
              return (
                <div className="list-item" key={d.id}>
                  <span className="tile-icon">
                    <Icon name={PLATFORM_ICON[d.platform] ?? 'monitor'} />
                  </span>
                  <div className="grow">
                    <div className="title truncate">{d.name}</div>
                    <div className="meta">
                      {PLATFORMS[d.platform] ?? d.platform}{d.agentVersion ? ` · агент ${d.agentVersion}` : ''}
                    </div>
                  </div>
                  <Badge tone={h.tone} dot>{h.text}</Badge>
                  {!readOnly && (
                    <ConfirmButton confirm="Точно отвязать?" onConfirm={() => revoke(d.id, d.name)}>
                      Отвязать
                    </ConfirmButton>
                  )}
                </div>
              );
            })}
          </div>
        )}
        <p className="small muted" style={{ marginTop: 12 }}>
          Отвязанное устройство теряет доступ сразу. Если токен попал не в те руки — отвяжите и выдайте новый.
        </p>
      </Card>

      {!readOnly && (
        <Card title="Привязать устройство" desc="Токен показывается один раз: в базе остаётся только его хеш.">
          <div className="stack">
            <ErrorBox message={err} />
            <Tabs<Platform>
              label="Платформа"
              segmented
              value={platform}
              onChange={(p) => { setPlatform(p); setName(p === 'windows' ? 'ПК ребёнка' : p === 'web' ? 'Браузер' : 'Телефон'); }}
              tabs={[
                { key: 'windows', label: 'Windows', icon: 'monitor' },
                { key: 'web', label: 'Браузер', icon: 'globe' },
                { key: 'android', label: 'Android', icon: 'phone' },
              ]}
            />
            <div className="row top">
              <Field label="Название" htmlFor="dev-name" className="grow" hint="Чтобы отличать устройства в списке.">
                <input id="dev-name" className="input" value={name} maxLength={60} onChange={(e) => setName(e.target.value)} />
              </Field>
              <button type="button" className="btn" style={{ marginTop: 26 }} disabled={busy} onClick={() => void enroll()}>
                <Icon name="key" />Выдать токен
              </button>
            </div>
            {issued && (
              <Alert tone="success" icon="key">
                <div className="stack" style={{ ['--gap' as string]: '10px' }}>
                  <strong>Токен выдан. Скопируйте его сейчас — второй раз он не покажется.</strong>
                  <div className="code-box">
                    <span className="mono" data-testid="device-token">{issued.token}</span>
                    <CopyButton text={issued.token} />
                  </div>
                  <NextSteps platform={issued.platform} />
                </div>
              </Alert>
            )}
          </div>
        </Card>
      )}
    </>
  );
}
