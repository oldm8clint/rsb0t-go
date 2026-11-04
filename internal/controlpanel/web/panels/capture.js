import { anyOf, el, button } from '../util.js';

export default {
    type:'capture', title:'Game view',
    description:'Capture full game graphics from the selected headless account, on demand. Open the viewer or download the PNG.',
    size:{w:6,h:4}, accounts:'one', settings:[],
    create(body,ctx) {
        const controls=el('div','controls');
        const image=el('img'); image.alt='Selected account game view'; image.hidden=true; image.className='capture-preview';
        const message=el('p','','Select an account, then capture its game view.');
        const download=el('a','','Download PNG'); download.hidden=true;
        let account='';
        const open=button('Capture game view','Render full game graphics on demand');
        open.addEventListener('click',()=>{
            if (!account) return;
            const popup=window.open(`capture.html?account=${encodeURIComponent(account)}`,'rs2bot-account-capture','width=850,height=760');
            message.textContent=popup ? 'Rendering in the game-view window…' : 'Allow the game-view popup to capture this account.';
        });
        controls.append(open,download); body.append(controls,image,message);
        const receive=event=>{
            if (event.origin !== location.origin || event.data?.type !== 'account-capture' || event.data.account !== account) return;
            if (typeof event.data.png !== 'string' || !event.data.png.startsWith('data:image/png;base64,') || event.data.png.length>4*1024*1024) return;
            image.src=event.data.png; image.hidden=false;
            download.href=event.data.png; download.download=`${account}-${event.data.capturedAt}.png`; download.hidden=false;
            message.textContent=`Captured ${new Date(event.data.capturedAt).toLocaleString()}. Click Capture game view to refresh.`;
        };
        window.addEventListener('message',receive);
        return {
            update(store,why) {
                if (!anyOf(why,'init','select','status')) return;
                const selected=ctx.accounts[0] || store.selected;
                if (account !== selected) { image.hidden=true; download.hidden=true; account=selected; message.textContent='Capture on demand; no continuous account rendering.'; }
                open.disabled=!account || store.byName.get(account)?.state !== 'ingame';
                ctx.setSub(account || 'No account selected');
            },
            destroy() { window.removeEventListener('message',receive); }
        };
    }
};
