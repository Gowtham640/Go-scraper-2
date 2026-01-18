const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

async function performLogin() {
  console.error('🔄 STEP 1: Reading environment variables...');
  const startTime = Date.now();

  const email = process.env.SRM_EMAIL;
  const password = process.env.SRM_PASSWORD;
  const timeout = parseInt(process.env.TIMEOUT_SECONDS || '30') * 1000;

  console.error(`📧 Email configured: ${email ? 'YES' : 'NO'}`);
  console.error(`🔑 Password configured: ${password ? 'YES' : 'NO'}`);
  console.error(`⏱️  Overall timeout: ${timeout / 1000} seconds`);

  if (!email || !password) {
    console.error('❌ MISSING_CREDENTIALS: Email or password not provided');
    console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
    console.log(JSON.stringify({
      status: 'error',
      reason: 'MISSING_CREDENTIALS'
    }));
    process.exit(1);
  }

  console.error('✅ Environment variables validated');
  console.error(`⏱️  Step 1 duration: ${Date.now() - startTime}ms`);
  console.error('');

  let browser = null;
  let context = null;
  let page = null;

  try {
    console.error('🔄 STEP 2: Launching Playwright browser...');
    const step2Start = Date.now();

    // Launch browser
    browser = await chromium.launch({
      headless: false,
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--disable-dev-shm-usage',
        '--disable-accelerated-2d-canvas',
        '--no-first-run',
        '--no-zygote',
        '--disable-gpu'
      ]
    });

    console.error('✅ Browser launched successfully');
    console.error(`⏱️  Step 2 duration: ${Date.now() - step2Start}ms`);
    console.error('');

    console.error('🔄 STEP 3: Creating browser context and page...');
    const step3Start = Date.now();

    // Create context and page
    context = await browser.newContext({
      viewport: { width: 1280, height: 720 },
      userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    });

    page = await context.newPage();
    console.error('✅ Browser context and page created');
    console.error('📐 Viewport: 1280x720');
    console.error('🤖 User Agent set');
    console.error(`⏱️  Step 3 duration: ${Date.now() - step3Start}ms`);
    console.error('');

    console.error('🔄 STEP 4: Navigating to SRM Academia portal...');
    console.error('🌐 URL: https://academia.srmist.edu.in/');
    console.error('⏳ Wait condition: networkidle');
    const step4Start = Date.now();

    // Navigate to login page
    await page.goto('https://academia.srmist.edu.in/', {
      waitUntil: 'networkidle',
      timeout: timeout
    });

    console.error('✅ Page navigation completed');
    console.error(`📄 Current URL: ${page.url()}`);

    // Get page title and basic info
    const title = await page.title();
    console.error(`📋 Page title: "${title}"`);

    // Check if page loaded correctly
    if (!page.url().includes('academia.srmist.edu.in')) {
      console.error(`⏱️  Step 4 duration: ${Date.now() - step4Start}ms`);
      throw new Error('PAGE_LOAD_FAILED: Not on expected domain');
    }
    console.error(`⏱️  Step 4 duration: ${Date.now() - step4Start}ms`);
    console.error('');

    console.error('🔄 STEP 5: Capturing page HTML for analysis...');
    const step5Start = Date.now();

    // Get the full HTML content of the page
    const pageHTML = await page.content();
    console.error(`📄 Page HTML length: ${pageHTML.length} characters`);
    console.error('💾 Saving HTML to signup.html...');

    // Save to signup.html file
    const fs = require('fs');
    const path = require('path');
    fs.writeFileSync(path.join(__dirname, '..', 'signup.html'), pageHTML);
    console.error('✅ HTML saved to signup.html');
    console.error(`⏱️  Step 5 duration: ${Date.now() - step5Start}ms`);
    console.error('');

    console.error('🔄 STEP 6: Waiting for iframe to load...');
    console.error('🎯 Selector: iframe#signinFrame');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const step6Start = Date.now();

    // Wait for iframe to be visible
    await page.waitForSelector('iframe#signinFrame');

    console.error('✅ Iframe found and loaded');
    console.error(`⏱️  Step 6 duration: ${Date.now() - step6Start}ms`);
    console.error('');

    console.error('🔄 STEP 7: Creating iframe locator...');
    console.error('🎯 Frame selector: iframe#signinFrame');
    const step7Start = Date.now();

    // Create frame locator for the signin iframe
    const signinFrame = page.frameLocator('iframe#signinFrame');

    console.error('✅ Iframe locator created');
    console.error(`⏱️  Step 7 duration: ${Date.now() - step7Start}ms`);
    console.error('');

    console.error('🔄 STEP 8: Looking for signin box inside iframe...');
    console.error('🎯 Selector: div.signin_box#signin_flow');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const step8Start = Date.now();

    // Wait for signin box to be visible inside the iframe
    await signinFrame.locator('div.signin_box#signin_flow').waitFor();

    console.error('✅ Signin box found and visible inside iframe');
    console.error(`⏱️  Step 8 duration: ${Date.now() - step8Start}ms`);
    console.error('');

    console.error('🔄 STEP 9: Filling email address...');
    console.error('📧 Email input selector: #login_id (inside iframe)');
    const step9Start = Date.now();

    // Fill email inside iframe
    await signinFrame.locator('#login_id').fill(email);
    console.error(`✅ Email filled: ${email.replace(/./g, '*').substring(0, 3)}***@***`);
    console.error(`⏱️  Step 9 duration: ${Date.now() - step9Start}ms`);
    console.error('');

    console.error('🔄 STEP 10: Clicking Next button...');
    console.error('🔘 Button selector: button#nextbtn:has-text("Next") (inside iframe)');
    const step10Start = Date.now();

    // Click Next button inside iframe
    await signinFrame.locator('button#nextbtn:has-text("Next")').click();
    console.error('✅ Next button clicked');
    console.error(`⏱️  Step 10 duration: ${Date.now() - step10Start}ms`);
    console.error('');

    console.error('🔄 STEP 11: Waiting for password field to appear...');
    console.error('🔑 Password input selector: #password (inside iframe)');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const step11Start = Date.now();

    // Wait for password field to appear inside iframe (page updates dynamically)
    await signinFrame.locator('#password').waitFor();

    console.error('✅ Password field appeared');
    console.error(`⏱️  Step 11 duration: ${Date.now() - step11Start}ms`);
    console.error('');

    console.error('🔄 STEP 12: Filling password...');
    console.error('🔑 Password input selector: #password (inside iframe)');
    const step12Start = Date.now();

    // Fill password inside iframe
    await signinFrame.locator('#password').fill(password);
    console.error(`✅ Password filled: ${'*'.repeat(password.length)}`);
    console.error(`⏱️  Step 12 duration: ${Date.now() - step12Start}ms`);
    console.error('');

    console.error('🔄 STEP 13: Clicking Sign In button...');
    console.error('🔘 Button selector: button#nextbtn (Sign In) (inside iframe)');
    const step13Start = Date.now();

    // Click Sign In button inside iframe (same button, now shows "Sign In")
    await signinFrame.locator('button#nextbtn').click();
    console.error('✅ Sign In button clicked');
    console.error(`⏱️  Step 13 duration: ${Date.now() - step13Start}ms`);
    console.error('');

    console.error('🔄 STEP 14: Waiting for login result...');
    console.error('⏳ Waiting for redirect to either:');
    console.error('   ✅ Success: /portal/academia-academic-services');
    console.error('   ⚠️  Rate limit: /announcement/signin-block');
    console.error('   🔄 Session limit: /preannouncement/block-sessions');
    console.error('⏳ No timeout limit - waiting indefinitely');
    const stepCookiesStart = Date.now();

    // Wait for navigation or rate limit page or session limit page
    try {
      await page.waitForURL(
        (url) => {
          // Success: portal home page
          if (url.href.includes('/portal/academia-academic-services')) {
            return true;
          }
          // Rate limit: blocked page
          if (url.href.includes('/accounts/p/') && url.href.includes('/announcement/signin-block')) {
            return true;
          }
          // Session limit: block-sessions page
          if (url.href.includes('/accounts/p/') && url.href.includes('/preannouncement/block-sessions')) {
            return true;
          }
          // Session limit: sessions-reminder page
          if (url.href.includes('/accounts/p/') && url.href.includes('/announcement/sessions-reminder')) {
            return true;
          }
          return false;
        }
      );

      console.error(`⏱️  Step 14 duration: ${Date.now() - stepCookiesStart}ms`);
      const finalUrl = page.url();
      console.error(`✅ Redirect detected to: ${finalUrl}`);

      if (finalUrl.includes('/portal/academia-academic-services')) {
        console.error('🎉 LOGIN SUCCESS: Redirected to portal home page');

        // Establish WMS session and navigate to timetable page
        try {
          console.error('🔄 STEP 14: Establishing WMS session and navigating to timetable...');

          // 🔐 Wait for authenticated API call (real session validation)
          await page.waitForResponse(response => {
            const url = response.url();
            return (
              response.status() === 200 &&
              url.includes('/srm_university/academia-academic-services/') &&
              response.headers()['set-cookie'] // Must set session cookies
            );
          }, { timeout: 15000 });

          console.error('✅ Authenticated session validated');

          // Now safe to navigate inside SPA
          console.error('🔗 Navigating inside app to My_Time_Table_Attendance');
          await page.evaluate(() => {
            window.location.hash = 'My_Time_Table_Attendance';
          });

          console.error('✅ Navigation completed');

          console.error('🔄 STEP 14.1: Waiting for WMS session initialization...');

          try {
            // This waits for the specific background API call that issues the WMS token
            await page.waitForResponse(response => {
              return response.url().includes('wms') ||
                (response.headers()['set-cookie'] && response.headers()['set-cookie'].includes('wms-tkp-token'));
            }, { timeout: 10000 });

            console.error('✅ WMS Token detected in network traffic');
          } catch (e) {
            console.error('⚠️ WMS network response not detected, falling back to element check');
            // Fallback: Wait for a specific UI element on the attendance page to be visible
            await page.waitForSelector('.custom_table, #attendance_det', { timeout: 5000 }).catch(() => { });
          }

          // Final safety buffer to allow the browser context to register the cookie
          await new Promise(resolve => setTimeout(resolve, 3000));

        } catch (navigationError) {
          console.error('⚠️  Session establishment or navigation failed, continuing with current page');
          console.error(`Error: ${navigationError.message}`);
          // Continue to cookie extraction even if navigation fails
        }

      } else if (finalUrl.includes('/announcement/signin-block')) {
        console.error('⚠️  RATE LIMIT DETECTED: On rate limit page');
      } else if (finalUrl.includes('/preannouncement/block-sessions')) {
        console.error('🔄 SESSION LIMIT DETECTED: On session limit page');
      } else if (finalUrl.includes('/announcement/sessions-reminder')) {
        console.error('🔄 SESSION LIMIT DETECTED: On sessions reminder page');
      }

    } catch (e) {
      console.error(`⏱️  Step 14 duration: ${Date.now() - stepCookiesStart}ms`);
      console.error('⚠️  No expected redirect detected');
      console.error('🔍 Checking current page URL...');

      // If we don't get expected redirects, check current URL
      const currentUrl = page.url();
      console.error(`📍 Current URL: ${currentUrl}`);

      if (currentUrl.includes('/announcement/signin-block')) {
        console.error('⚠️  RATE LIMIT: On rate limit page, clicking continue...');
        const rateLimitStart = Date.now();

        // Handle rate limit page
        await page.click('a#continue_button');
        console.error('✅ Continue button clicked');

        console.error('⏳ Waiting for redirect after rate limit bypass...');
        await page.waitForURL(
          (url) => url.href.includes('/portal/academia-academic-services')
        );

        console.error(`⏱️  Rate limit bypass duration: ${Date.now() - rateLimitStart}ms`);
        const afterRateLimitUrl = page.url();
        console.error(`✅ Redirected to: ${afterRateLimitUrl}`);

        if (afterRateLimitUrl.includes('/portal/academia-academic-services')) {
          console.error('🎉 RATE LIMIT BYPASSED: Successfully logged in');

          // Establish WMS session and navigate to timetable page
          try {
            console.error('🔄 STEP 14: Establishing WMS session and navigating to timetable...');

            // 🔐 Wait for authenticated API call (real session validation)
            await page.waitForResponse(response => {
              const url = response.url();
              return (
                response.status() === 200 &&
                url.includes('/srm_university/academia-academic-services/') &&
                response.headers()['set-cookie'] // Must set session cookies
              );
            }, { timeout: 15000 });

            console.error('✅ Authenticated session validated');

            // Step 1: Navigate via real route, not hash
            console.error('🔗 Navigating to My_Time_Table_Attendance via real route');
            await page.goto(
              'https://academia.srmist.edu.in/portal/academia-academic-services/My_Time_Table_Attendance',
              { waitUntil: 'networkidle' }
            );

            console.error('✅ Navigation completed');

            // Step 2: Explicitly wait for WMS API calls
            await page.waitForResponse(response => {
              return (
                response.url().includes('/wms/') &&
                response.status() === 200
              );
            }, { timeout: 30000 });

            console.error('✅ WMS API calls completed');

            // Step 3: Verify cookie appearance BEFORE extraction
            await page.waitForFunction(() =>
              document.cookie.includes('wms-tkp-token'),
              { timeout: 15000 }
            );

            console.error('✅ WMS cookies verified');

          } catch (navigationError) {
            console.error('⚠️  Session establishment or navigation failed, continuing with current page');
            console.error(`Error: ${navigationError.message}`);
            // Continue to cookie extraction even if navigation fails
          }

        } else {
          throw new Error('RATE_LIMIT_BYPASS_FAILED');
        }

      } else if (currentUrl.includes('/preannouncement/block-sessions')) {
        console.error('🔄 SESSION LIMIT: On session limit page, terminating other sessions...');
        const sessionLimitStart = Date.now();

        // Handle session limit page - click "Terminate all other sessions"
        console.error('🔍 Looking for "Terminate all other sessions" button...');

        // Always prioritize text-based selection for reliability
        try {
          console.error('🔍 URL before button click:', page.url());
          // Wait for the button to be visible and clickable - use exact text from HTML
          await page.waitForSelector('text="Terminate All Sessions"', { timeout: 5000 });
          await page.click('text="Terminate All Sessions"');
          console.error('✅ Terminate All Sessions button clicked (by text)');

          // Brief pause to let any immediate redirects happen
          await new Promise(resolve => setTimeout(resolve, 1000));
          console.error('🔍 URL 1 second after button click:', page.url());
        } catch (textError) {
          console.error('⚠️ Text selector failed, trying exact selector from current HTML...');
          try {
            // Try the exact selector from the current HTML: a.blue_btn.continue_button#continue_button
            await page.waitForSelector('a.blue_btn.continue_button#continue_button', { timeout: 5000 });
            await page.click('a.blue_btn.continue_button#continue_button');
            console.error('✅ Terminate All Sessions button clicked (by exact class + ID)');

            // Brief pause to let any immediate redirects happen
            await new Promise(resolve => setTimeout(resolve, 1000));
            console.error('🔍 URL 1 second after button click:', page.url());
          } catch (exactError) {
            console.error('❌ Exact selector failed, trying fallback selectors...');
            // Try fallback selectors with the correct text
            const altSelectors = ['text="Terminate All Sessions"', 'a#continue_button', '.blue_btn'];
            let clicked = false;
            for (const selector of altSelectors) {
              try {
                console.error('🔍 URL before alternative click:', page.url());
                await page.waitForSelector(selector, { timeout: 3000 });
                await page.click(selector);
                console.error(`✅ Clicked using fallback selector: ${selector}`);

                // Brief pause to let any immediate redirects happen
                await new Promise(resolve => setTimeout(resolve, 1000));
                console.error('🔍 URL 1 second after alternative click:', page.url());
                clicked = true;
                break;
              } catch (e) {
                console.error(`❌ Fallback selector failed: ${selector}`);
              }
            }
            if (!clicked) {
              throw new Error('Could not find or click terminate sessions button');
            }
          }
        }

        console.error('⏳ Waiting for redirect after session termination...');
        try {
          await page.waitForURL(
            (url) => url.href.includes('/portal/academia-academic-services'),
            { timeout: 30000 }
          );
        } catch (redirectError) {
          console.error('⚠️ Expected redirect did not occur, checking current URL...');
          const currentUrl = page.url();
          console.error(`📍 Current URL after button click: ${currentUrl}`);

          // If we're still on a session limit page, something went wrong
          if (currentUrl.includes('/announcement/') || currentUrl.includes('/preannouncement/')) {
            throw new Error(`Failed to redirect from session limit page. Current URL: ${currentUrl}`);
          }

          // If we're already on dashboard, continue
          if (currentUrl.includes('/portal/academia-academic-services')) {
            console.error('✅ Already on dashboard, continuing...');
          } else {
            throw new Error(`Unexpected URL after session termination: ${currentUrl}`);
          }
        }

        console.error(`⏱️  Session limit bypass duration: ${Date.now() - sessionLimitStart}ms`);
        const afterSessionLimitUrl = page.url();
        console.error(`✅ Redirected to: ${afterSessionLimitUrl}`);

        if (afterSessionLimitUrl.includes('/portal/academia-academic-services')) {
          console.error('🎉 SESSION LIMIT BYPASSED: Successfully logged in');

          // Establish WMS session and navigate to timetable page
          try {
            console.error('🔄 STEP 14: Establishing WMS session and navigating to timetable...');

            // 🔐 Wait for authenticated API call (real session validation)
            await page.waitForResponse(response => {
              const url = response.url();
              return (
                response.status() === 200 &&
                url.includes('/srm_university/academia-academic-services/') &&
                response.headers()['set-cookie'] // Must set session cookies
              );
            }, { timeout: 15000 });

            console.error('✅ Authenticated session validated');

            // Step 1: Navigate via real route, not hash
            console.error('🔗 Navigating to My_Time_Table_Attendance via real route');
            await page.goto(
              'https://academia.srmist.edu.in/portal/academia-academic-services/My_Time_Table_Attendance',
              { waitUntil: 'networkidle' }
            );

            console.error('✅ Navigation completed');

            // Step 2: Explicitly wait for WMS API calls
            await page.waitForResponse(response => {
              return (
                response.url().includes('/wms/') &&
                response.status() === 200
              );
            }, { timeout: 30000 });

            console.error('✅ WMS API calls completed');

            // Step 3: Verify cookie appearance BEFORE extraction
            await page.waitForFunction(() =>
              document.cookie.includes('wms-tkp-token'),
              { timeout: 15000 }
            );

            console.error('✅ WMS cookies verified');

          } catch (navigationError) {
            console.error('⚠️  Session establishment or navigation failed, continuing with current page');
            console.error(`Error: ${navigationError.message}`);
            // Continue to cookie extraction even if navigation fails
          }

        } else {
          throw new Error('SESSION_LIMIT_BYPASS_FAILED');
        }

      } else if (currentUrl.includes('/announcement/sessions-reminder')) {
        console.error('🔄 SESSION LIMIT: On sessions reminder page, terminating other sessions...');
        const sessionLimitStart = Date.now();

        // Handle sessions reminder page - click "Terminate all other sessions"
        console.error('🔍 Looking for "Terminate all other sessions" button...');

        // Always prioritize text-based selection for reliability
        try {
          console.error('🔍 URL before button click:', page.url());
          // Wait for the button to be visible and clickable - use exact text from HTML
          await page.waitForSelector('text="Terminate All Sessions"', { timeout: 5000 });
          await page.click('text="Terminate All Sessions"');
          console.error('✅ Terminate All Sessions button clicked (by text)');

          // Brief pause to let any immediate redirects happen
          await new Promise(resolve => setTimeout(resolve, 1000));
          console.error('🔍 URL 1 second after button click:', page.url());
        } catch (textError) {
          console.error('⚠️ Text selector failed, trying exact selector from current HTML...');
          try {
            // Try the exact selector from the current HTML: a.blue_btn.continue_button#continue_button
            await page.waitForSelector('a.blue_btn.continue_button#continue_button', { timeout: 5000 });
            await page.click('a.blue_btn.continue_button#continue_button');
            console.error('✅ Terminate All Sessions button clicked (by exact class + ID)');

            // Brief pause to let any immediate redirects happen
            await new Promise(resolve => setTimeout(resolve, 1000));
            console.error('🔍 URL 1 second after button click:', page.url());
          } catch (exactError) {
            console.error('❌ Exact selector failed, trying fallback selectors...');
            // Try fallback selectors with the correct text
            const altSelectors = ['text="Terminate All Sessions"', 'a#continue_button', '.blue_btn'];
            let clicked = false;
            for (const selector of altSelectors) {
              try {
                console.error('🔍 URL before alternative click:', page.url());
                await page.waitForSelector(selector, { timeout: 3000 });
                await page.click(selector);
                console.error(`✅ Clicked using fallback selector: ${selector}`);

                // Brief pause to let any immediate redirects happen
                await new Promise(resolve => setTimeout(resolve, 1000));
                console.error('🔍 URL 1 second after alternative click:', page.url());
                clicked = true;
                break;
              } catch (e) {
                console.error(`❌ Fallback selector failed: ${selector}`);
              }
            }
            if (!clicked) {
              throw new Error('Could not find or click terminate sessions button');
            }
          }
        }

        console.error('⏳ Waiting for redirect after session termination...');
        try {
          await page.waitForURL(
            (url) => url.href.includes('/portal/academia-academic-services'),
            { timeout: 30000 }
          );
        } catch (redirectError) {
          console.error('⚠️ Expected redirect did not occur, checking current URL...');
          const currentUrl = page.url();
          console.error(`📍 Current URL after button click: ${currentUrl}`);

          // If we're still on a session limit page, something went wrong
          if (currentUrl.includes('/announcement/') || currentUrl.includes('/preannouncement/')) {
            throw new Error(`Failed to redirect from session limit page. Current URL: ${currentUrl}`);
          }

          // If we're already on dashboard, continue
          if (currentUrl.includes('/portal/academia-academic-services')) {
            console.error('✅ Already on dashboard, continuing...');
          } else {
            throw new Error(`Unexpected URL after session termination: ${currentUrl}`);
          }
        }

        console.error(`⏱️  Session limit bypass duration: ${Date.now() - sessionLimitStart}ms`);
        const afterSessionLimitUrl = page.url();
        console.error(`✅ Redirected to: ${afterSessionLimitUrl}`);

        if (afterSessionLimitUrl.includes('/portal/academia-academic-services')) {
          console.error('🎉 SESSION LIMIT BYPASSED: Successfully logged in');

          // Establish WMS session and navigate to timetable page
          try {
            console.error('🔄 STEP 14: Establishing WMS session and navigating to timetable...');

            // 🔐 Wait for authenticated API call (real session validation)
            await page.waitForResponse(response => {
              const url = response.url();
              return (
                response.status() === 200 &&
                url.includes('/srm_university/academia-academic-services/') &&
                response.headers()['set-cookie'] // Must set session cookies
              );
            }, { timeout: 15000 });

            console.error('✅ Authenticated session validated');

            // Step 1: Navigate via real route, not hash
            console.error('🔗 Navigating to My_Time_Table_Attendance via real route');
            await page.goto(
              'https://academia.srmist.edu.in/portal/academia-academic-services/My_Time_Table_Attendance',
              { waitUntil: 'networkidle' }
            );

            console.error('✅ Navigation completed');

            // Step 2: Explicitly wait for WMS API calls
            await page.waitForResponse(response => {
              return (
                response.url().includes('/wms/') &&
                response.status() === 200
              );
            }, { timeout: 30000 });

            console.error('✅ WMS API calls completed');

            // Step 3: Verify cookie appearance BEFORE extraction
            await page.waitForFunction(() =>
              document.cookie.includes('wms-tkp-token'),
              { timeout: 15000 }
            );

            console.error('✅ WMS cookies verified');

          } catch (navigationError) {
            console.error('⚠️  Session establishment or navigation failed, continuing with current page');
            console.error(`Error: ${navigationError.message}`);
            // Continue to cookie extraction even if navigation fails
          }

        } else {
          throw new Error('SESSION_LIMIT_BYPASS_FAILED');
        }

      } else if (!currentUrl.includes('/portal/academia-academic-services')) {
        console.error('❌ LOGIN FAILED: Not on expected success page');
        throw new Error('LOGIN_FAILED');
      } else {
        console.error('🎉 LOGIN SUCCESS: Already on portal page');

        // Establish WMS session and navigate to timetable page
        try {
          console.error('🔄 STEP 14: Establishing WMS session and navigating to timetable...');

          // 🔐 Wait for authenticated API call (real session validation)
          await page.waitForResponse(response => {
            const url = response.url();
            return (
              response.status() === 200 &&
              url.includes('/srm_university/academia-academic-services/') &&
              response.headers()['set-cookie'] // Must set session cookies
            );
          }, { timeout: 15000 });

          console.error('✅ Authenticated session validated');

          // Step 1: Navigate via real route, not hash
          console.error('🔗 Navigating to My_Time_Table_Attendance via real route');
          await page.goto(
            'https://academia.srmist.edu.in/portal/academia-academic-services/My_Time_Table_Attendance',
            { waitUntil: 'networkidle' }
          );

          console.error('✅ Navigation completed');

          // Step 2: Explicitly wait for WMS API calls
          await page.waitForResponse(response => {
            return (
              response.url().includes('/wms/') &&
              response.status() === 200
            );
          }, { timeout: 30000 });

          console.error('✅ WMS API calls completed');

          // Step 3: Verify cookie appearance BEFORE extraction
          await page.waitForFunction(() =>
            document.cookie.includes('wms-tkp-token'),
            { timeout: 15000 }
          );

          console.error('✅ WMS cookies verified');

        } catch (navigationError) {
          console.error('⚠️  Session establishment or navigation failed, continuing with current page');
          console.error(`Error: ${navigationError.message}`);
          // Continue to cookie extraction even if navigation fails
        }
      }
    }

    console.error('');

    console.error('🔄 STEP 15: Final WMS stabilization and cookie extraction...');
    const cookieExtractStart = Date.now();

    // Wait for successful /wms/ network response
    console.error('⏳ Waiting for WMS API response...');
    await page.waitForResponse(response => {
      return (
        response.url().includes('/wms/') &&
        response.status() === 200
      );
    }, { timeout: 30000 });

    console.error('✅ WMS API response received');

    // Wait until document.cookie contains wms-tkp-token
    console.error('⏳ Waiting for wms-tkp-token in document.cookie...');
    await page.waitForFunction(() =>
      document.cookie.includes('wms-tkp-token'),
      { timeout: 15000 }
    );

    console.error('✅ wms-tkp-token found in document.cookie');

    // Short stabilization delay after cookie appears
    console.error('⏳ Stabilization delay (1 second)...');
    await new Promise(resolve => setTimeout(resolve, 1000));

    console.error('🔄 STEP 16: Extracting session cookies...');

    // Extract all cookies from browser context
    const cookies = await context.cookies();
    console.error(`🍪 Found ${cookies.length} cookies from browser context`);

    // Also try to extract cookies from the page (in case some are page-specific)
    let pageCookies = [];
    try {
      pageCookies = await page.context().cookies();
      if (pageCookies.length !== cookies.length) {
        console.error(`🍪 Page context has ${pageCookies.length} cookies (different from browser context)`);
      }
    } catch (e) {
      console.error('⚠️ Could not extract page cookies:', e.message);
    }

    // Log cookie details for debugging
    const cookieNames = cookies.map(cookie => cookie.name);
    const cookieDomains = [...new Set(cookies.map(cookie => cookie.domain))];
    console.error(`🍪 Cookie names: ${cookieNames.join(', ')}`);
    console.error(`🌐 Cookie domains: ${cookieDomains.join(', ')}`);

    // Check for essential cookies
    const essentialCookies = ['JSESSIONID', 'iamcsr', 'zccpn', '_zcsr_tmp', 'stk', '__Secure-iamsdt_client_10002227248', '_iamadt_client_10002227248', '_iambdt_client_10002227248', 'wms-tkp-token_client_10002227248'];
    const foundEssential = essentialCookies.filter(name => cookieNames.includes(name));
    const missingEssential = essentialCookies.filter(name => !cookieNames.includes(name));

    console.error(`✅ Found essential cookies: ${foundEssential.join(', ')}`);
    if (missingEssential.length > 0) {
      console.error(`❌ Missing essential cookies: ${missingEssential.join(', ')}`);
    }

    const formattedCookies = cookies.map(cookie => ({
      name: cookie.name,
      value: cookie.value,
      domain: cookie.domain,
      path: cookie.path,
      httpOnly: cookie.httpOnly,
      secure: cookie.secure,
      expiry: cookie.expires
    }));

    console.error(`⏱️  Step 16 duration: ${Date.now() - cookieExtractStart}ms`);
    console.error('');
    console.error('🎉 LOGIN PROCESS COMPLETED SUCCESSFULLY');
    console.error(`⏱️  TOTAL DURATION: ${Date.now() - startTime}ms`);

    // Output success JSON
    console.log(JSON.stringify({
      status: 'success',
      timestamp: new Date().toISOString(),
      cookies: formattedCookies
    }));

    process.exit(0);

  } catch (error) {
    console.error('');
    console.error('❌ LOGIN PROCESS FAILED');
    console.error(`💥 Error: ${error.message}`);
    console.error(`⏱️  TOTAL DURATION BEFORE FAILURE: ${Date.now() - startTime}ms`);

    let reason = 'UNKNOWN_ERROR';

    if (error.message.includes('PAGE_LOAD_FAILED')) {
      reason = 'PAGE_LOAD_FAILED';
      console.error('🔍 DIAGNOSIS: Page did not load correctly (wrong domain)');
    } else if (error.message.includes('selector') || error.message.includes('not found')) {
      reason = 'SELECTOR_NOT_FOUND';
      console.error('🔍 DIAGNOSIS: Could not find expected HTML elements on page');
      console.error('   Possible causes:');
      console.error('   - SRM portal UI changed');
      console.error('   - Page took too long to load');
      console.error('   - Network connectivity issues');
    } else if (error.message.includes('timeout') || error.message.includes('Navigation timeout')) {
      reason = 'TIMEOUT';
      console.error('🔍 DIAGNOSIS: Operation timed out');
      console.error('   Possible causes:');
      console.error('   - Slow network connection');
      console.error('   - SRM portal slow/unresponsive');
      console.error('   - Browser performance issues');
    } else if (error.message.includes('LOGIN_FAILED')) {
      reason = 'LOGIN_FAILED';
      console.error('🔍 DIAGNOSIS: Login credentials rejected');
      console.error('   Possible causes:');
      console.error('   - Invalid email/password');
      console.error('   - Account locked/disabled');
      console.error('   - SRM portal authentication issues');
    } else if (error.message.includes('RATE_LIMIT_BYPASS_FAILED')) {
      reason = 'RATE_LIMIT_BLOCKED';
      console.error('🔍 DIAGNOSIS: Rate limit bypass failed');
      console.error('   Possible causes:');
      console.error('   - Too many login attempts');
      console.error('   - Rate limit page structure changed');
    } else if (error.message.includes('SESSION_LIMIT_BYPASS_FAILED')) {
      reason = 'SESSION_LIMIT_BLOCKED';
      console.error('🔍 DIAGNOSIS: Session limit bypass failed');
      console.error('   Possible causes:');
      console.error('   - Session termination failed');
      console.error('   - Session limit page structure changed');
    } else if (error.message.includes('net::')) {
      reason = 'NETWORK_ERROR';
      console.error('🔍 DIAGNOSIS: Network connectivity issues');
      console.error('   Possible causes:');
      console.error('   - No internet connection');
      console.error('   - SRM portal unreachable');
      console.error('   - Firewall/proxy blocking');
    }

    console.error('');
    console.error('📤 Returning error response...');

    console.log(JSON.stringify({
      status: 'error',
      timestamp: new Date().toISOString(),
      reason: reason,
      details: error.message
    }));

    process.exit(1);

  } finally {
    // Clean up
    if (page) await page.close().catch(() => { });
    if (context) await context.close().catch(() => { });
    if (browser) await browser.close().catch(() => { });
  }
}

// Run the login
performLogin();
