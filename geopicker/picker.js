// 地图选点: 搜索/点击/拖动落点 -> 高德 REST 逆地理编码(与金数据前端一致) -> 确定返回。
(function () {
  'use strict';

  var q = new URLSearchParams(location.search);
  var KEY = q.get('key') || '825d0697d0a22be408d41eb8ef59d5ac';
  var CENTER = [parseFloat(q.get('lat')) || 39.908, parseFloat(q.get('lng')) || 116.397];
  var ZOOM = parseInt(q.get('zoom'), 10) || 13;

  var PIN = '<svg viewBox="0 0 24 36" width="26" height="36">' +
    '<path fill="#e74c3c" stroke="#fff" stroke-width="1.5" ' +
    'd="M12 0C5.4 0 0 5.4 0 12c0 9 12 24 12 24s12-15 12-24C24 5.4 18.6 0 12 0z"/>' +
    '<circle cx="12" cy="12" r="4.5" fill="#fff"/></svg>';

  var map = L.map('map', { zoomControl: true }).setView(CENTER, ZOOM);
  L.tileLayer('https://webrd0{s}.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}', {
    subdomains: ['1', '2', '3', '4'],
    maxZoom: 19,
    attribution: '© 高德地图'
  }).addTo(map);

  var marker = L.marker(CENTER, {
    draggable: true,
    icon: L.divIcon({ className: '', iconSize: [26, 36], iconAnchor: [13, 36], html: PIN })
  }).addTo(map);

  var addr = '';

  // 逆地理编码(坐标 -> 地址), 与金数据前端同一个接口。
  function regeo(lat, lng) {
    document.getElementById('addr').textContent = '解析地址中...';
    fetch('https://restapi.amap.com/v3/geocode/regeo?key=' + KEY +
          '&s=rsv3&language=zh_cn&location=' + lng + ',' + lat)
      .then(function (r) { return r.json(); })
      .then(function (j) {
        addr = (j.regeocode && j.regeocode.formatted_address) || '';
        document.getElementById('addr').textContent = addr || '（未解析到地址）';
      })
      .catch(function () {
        addr = '';
        document.getElementById('addr').textContent = '（地址解析失败）';
      });
  }

  // 正地理编码(关键字 -> 坐标), 用于输入地点定位。
  function geocodeAddress(kw) {
    return fetch('https://restapi.amap.com/v3/geocode/geo?key=' + KEY +
                 '&s=rsv3&language=zh_cn&address=' + encodeURIComponent(kw))
      .then(function (r) { return r.json(); })
      .then(function (j) { return (j.geocodes && j.geocodes[0]) || null; });
  }

  function showPoint(lat, lng, zoom) {
    marker.setLatLng([lat, lng]);
    map.setView([lat, lng], zoom || Math.max(map.getZoom(), 15));
    document.getElementById('coord').textContent =
      '经度: ' + lng.toFixed(6) + '  纬度: ' + lat.toFixed(6);
    regeo(lat, lng);
  }

  function setFromMarker() {
    var p = marker.getLatLng();
    document.getElementById('coord').textContent =
      '经度: ' + p.lng.toFixed(6) + '  纬度: ' + p.lat.toFixed(6);
    regeo(p.lat, p.lng);
  }

  function search() {
    var kw = document.getElementById('kw').value.trim();
    if (!kw) return;
    document.getElementById('addr').textContent = '搜索中...';
    geocodeAddress(kw).then(function (g) {
      if (!g) { document.getElementById('addr').textContent = '未找到该地点'; return; }
      var loc = g.location.split(',');
      showPoint(parseFloat(loc[1]), parseFloat(loc[0]), 16);
    }).catch(function () {
      document.getElementById('addr').textContent = '搜索失败';
    });
  }

  document.getElementById('find').onclick = search;
  document.getElementById('kw').addEventListener('keydown', function (e) {
    if (e.key === 'Enter') { e.stopPropagation(); search(); }
  });

  marker.on('dragend', setFromMarker);
  map.on('click', function (e) { showPoint(e.latlng.lat, e.latlng.lng); });
  setFromMarker();

  document.getElementById('locate').onclick = function () {
    navigator.geolocation.getCurrentPosition(
      function (p) { showPoint(p.coords.latitude, p.coords.longitude, 17); },
      function (err) { alert('定位失败: ' + err.message); },
      { enableHighAccuracy: true, timeout: 8000 });
  };

  document.getElementById('ok').onclick = function () {
    var p = marker.getLatLng();
    fetch('/pick', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ latitude: p.lat, longitude: p.lng, address: addr })
    }).then(function () {
      document.body.innerHTML =
        '<div style="font:16px/1.8 sans-serif;padding:40px;text-align:center">' +
        '位置已选择，可关闭本页面<br><span style="color:#888;font-size:13px">' +
        '程序已收到经纬度并退出</span></div>';
    });
  };

  document.getElementById('cancel').onclick = function () { fetch('/cancel', { method: 'POST' }); };
  document.addEventListener('keydown', function (e) {
    if (e.target && e.target.tagName === 'INPUT') return;   // 输入框里回车不触发确定
    if (e.key === 'Enter') document.getElementById('ok').click();
    if (e.key === 'Escape') document.getElementById('cancel').click();
  });
})();
